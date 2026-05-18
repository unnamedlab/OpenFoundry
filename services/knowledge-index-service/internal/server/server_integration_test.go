//go:build integration

// End-to-end integration test for knowledge-index-service. Boots an
// ephemeral postgres:16-alpine via libs/testing.BootPostgres, applies
// the service migrations, wires the real PGKnowledgeStore through the
// chi router and exercises the full /api/v1/ai/knowledge-bases CRUD
// (knowledge base + document) round-trip over HTTP with a real JWT.
package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/server"
)

const integrationJWTSecret = "knowledge-index-integration-secret"

func mintIntegrationToken(t *testing.T) string {
	t.Helper()
	cfg := authmw.NewJWTConfig(integrationJWTSecret)
	use := "access"
	tok, err := authmw.EncodeToken(cfg, &authmw.Claims{
		Sub:      uuid.New(),
		IAT:      time.Now().Unix(),
		EXP:      time.Now().Add(time.Hour).Unix(),
		JTI:      uuid.New(),
		Email:    "kb-integration@example.com",
		Name:     "KB Integration",
		Roles:    []string{"user"},
		TokenUse: &use,
	})
	require.NoError(t, err)
	return tok
}

func authRequest(t *testing.T, method, url, token string, body []byte) *http.Request {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rdr == nil {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, rdr)
	}
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func decode(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

// TestKnowledgeIndexCRUDRoundTripPostgres covers POST/GET/LIST/PATCH/DELETE
// for knowledge bases plus POST/GET/LIST/DELETE for nested documents,
// all against a real Postgres pool with the embedded migration applied.
func TestKnowledgeIndexCRUDRoundTripPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))

	cfg := &config.Config{}
	cfg.Service.Name = "knowledge-index-service"
	cfg.Service.Version = "integration"
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.ShutdownTimeout = "5s"
	cfg.JWT.Secret = integrationJWTSecret

	srv, err := server.New(cfg, observability.NewMetrics(), nil, server.WithPostgresPool(pg.Pool))
	require.NoError(t, err)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	token := mintIntegrationToken(t)
	base := ts.URL + "/api/v1/ai/knowledge-bases"
	client := ts.Client()

	t.Run("healthz unauthenticated", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("metrics unauthenticated", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/metrics")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("api requires auth", func(t *testing.T) {
		resp, err := http.Get(base)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	var createdID uuid.UUID
	t.Run("create knowledge base", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":        "integration-kb",
			"description": "boots-postgres",
			"tags":        []string{"integration", "ci"},
		})
		resp, err := client.Do(authRequest(t, http.MethodPost, base, token, body))
		require.NoError(t, err)
		var kb struct {
			ID                uuid.UUID `json:"id"`
			Name              string    `json:"name"`
			Status            string    `json:"status"`
			EmbeddingProvider string    `json:"embedding_provider"`
			ChunkingStrategy  string    `json:"chunking_strategy"`
		}
		decode(t, resp, &kb)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "integration-kb", kb.Name)
		assert.Equal(t, "active", kb.Status)
		assert.Equal(t, "deterministic-hash", kb.EmbeddingProvider)
		assert.Equal(t, "balanced", kb.ChunkingStrategy)
		require.NotEqual(t, uuid.Nil, kb.ID)
		createdID = kb.ID
	})

	t.Run("get knowledge base", func(t *testing.T) {
		resp, err := client.Do(authRequest(t, http.MethodGet, fmt.Sprintf("%s/%s", base, createdID), token, nil))
		require.NoError(t, err)
		var kb map[string]any
		decode(t, resp, &kb)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "integration-kb", kb["name"])
	})

	t.Run("list knowledge bases", func(t *testing.T) {
		resp, err := client.Do(authRequest(t, http.MethodGet, base, token, nil))
		require.NoError(t, err)
		var page struct {
			Data []map[string]any `json:"data"`
		}
		decode(t, resp, &page)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Len(t, page.Data, 1)
	})

	t.Run("patch knowledge base", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"description": "updated-by-integration",
			"tags":        []string{"renamed"},
		})
		resp, err := client.Do(authRequest(t, http.MethodPatch, fmt.Sprintf("%s/%s", base, createdID), token, body))
		require.NoError(t, err)
		var kb map[string]any
		decode(t, resp, &kb)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "updated-by-integration", kb["description"])
		assert.Equal(t, []any{"renamed"}, kb["tags"])
	})

	var docID uuid.UUID
	t.Run("create document", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"title":   "first-doc",
			"content": "Postgres-backed retrieval index round-trip body.",
		})
		resp, err := client.Do(authRequest(t, http.MethodPost, fmt.Sprintf("%s/%s/documents", base, createdID), token, body))
		require.NoError(t, err)
		var doc struct {
			ID         uuid.UUID `json:"id"`
			Title      string    `json:"title"`
			Status     string    `json:"status"`
			ChunkCount int32     `json:"chunk_count"`
		}
		decode(t, resp, &doc)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "first-doc", doc.Title)
		assert.Equal(t, "indexed", doc.Status)
		assert.GreaterOrEqual(t, doc.ChunkCount, int32(1))
		docID = doc.ID
	})

	t.Run("list documents", func(t *testing.T) {
		resp, err := client.Do(authRequest(t, http.MethodGet, fmt.Sprintf("%s/%s/documents", base, createdID), token, nil))
		require.NoError(t, err)
		var page struct {
			Data []map[string]any `json:"data"`
		}
		decode(t, resp, &page)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Len(t, page.Data, 1)
	})

	t.Run("get document", func(t *testing.T) {
		resp, err := client.Do(authRequest(t, http.MethodGet, fmt.Sprintf("%s/%s/documents/%s", base, createdID, docID), token, nil))
		require.NoError(t, err)
		var doc map[string]any
		decode(t, resp, &doc)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "first-doc", doc["title"])
	})

	t.Run("search knowledge base", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"query": "retrieval index", "top_k": 3, "min_score": 0.0})
		resp, err := client.Do(authRequest(t, http.MethodPost, fmt.Sprintf("%s/%s/search", base, createdID), token, body))
		require.NoError(t, err)
		var out struct {
			Results []map[string]any `json:"results"`
		}
		decode(t, resp, &out)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotNil(t, out.Results)
	})

	t.Run("delete document", func(t *testing.T) {
		resp, err := client.Do(authRequest(t, http.MethodDelete, fmt.Sprintf("%s/%s/documents/%s", base, createdID, docID), token, nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("delete knowledge base", func(t *testing.T) {
		resp, err := client.Do(authRequest(t, http.MethodDelete, fmt.Sprintf("%s/%s", base, createdID), token, nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		resp, err = client.Do(authRequest(t, http.MethodGet, fmt.Sprintf("%s/%s", base, createdID), token, nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
