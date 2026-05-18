package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/server"
)

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(server.BuildRouter(newTestConfig(""), server.Deps{}, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "retrieval-context-service", body["service"])
}

func TestReadyzMounted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(server.BuildRouter(newTestConfig(""), server.Deps{}, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestUnmountedBusinessRoutesReturn401(t *testing.T) {
	// Wire-compat: knowledge-bases and conversations live in sibling
	// services. Hitting them here should pass through authmw first and
	// return 401, not 404.
	t.Parallel()
	srv := httptest.NewServer(server.BuildRouter(newTestConfig(""), server.Deps{}, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	for _, path := range []string{
		"/api/v1/knowledge-bases",
		"/api/v1/conversations",
	} {
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "path %s should not be a mounted business route", path)
	}
}

func TestAPIV1RequiresAuth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(server.BuildRouter(newTestConfig("retrieval-context-test-secret"), server.Deps{}, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/_authz_probe")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAPIV1AcceptsValidBearer(t *testing.T) {
	t.Parallel()
	const secret = "retrieval-context-test-secret-do-not-use-in-prod"
	srv := httptest.NewServer(server.BuildRouter(newTestConfig(secret), server.Deps{}, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	tok := mintAccessToken(t, secret)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/_authz_probe", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestDocumentIntelligenceJobsRoundTrip(t *testing.T) {
	t.Parallel()
	const secret = "retrieval-context-test-secret-do-not-use-in-prod"
	store := repo.NewMemoryStore()
	deps := server.Deps{
		Jobs: &handlers.Jobs{Store: store},
		JWT:  testJWTConfig(secret),
	}
	srv := httptest.NewServer(server.BuildRouter(newTestConfig(secret), deps, observability.NewMetrics()))
	t.Cleanup(srv.Close)
	tok := mintAccessToken(t, secret)

	// POST job
	createReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/document-intelligence/jobs",
		jsonBody(t, map[string]any{
			"source_uri": "s3://docs/contract.pdf",
			"pipeline":   "pdf-ocr",
		}))
	createReq.Header.Set("Authorization", "Bearer "+tok)
	createReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(createReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	jobID := created["id"].(string)
	assert.Equal(t, "queued", created["status"])

	// GET job
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/document-intelligence/jobs/"+jobID, nil)
	getReq.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(getReq)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// PATCH job -> running
	patchReq, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/document-intelligence/jobs/"+jobID,
		jsonBody(t, map[string]any{"status": "running"}))
	patchReq.Header.Set("Authorization", "Bearer "+tok)
	patchReq.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// DELETE job
	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/document-intelligence/jobs/"+jobID, nil)
	delReq.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(delReq)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()
}

func newTestConfig(secret string) *config.Config {
	cfg := &config.Config{}
	cfg.Service.Name = "retrieval-context-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = secret
	return cfg
}

func testJWTConfig(secret string) *authmw.JWTConfig {
	jwt := authmw.NewJWTConfig(secret)
	jwt.AccessTTL = time.Hour
	return jwt
}

func mintAccessToken(t *testing.T, secret string) string {
	t.Helper()
	jwt := testJWTConfig(secret)
	c := authmw.BuildAccessClaims(jwt, authmw.AccessClaimsInput{
		UserID: uuid.New(),
		Email:  "retrieval-test@example.com",
		Name:   "Retrieval Test",
		Roles:  []string{"user"},
	})
	tok, err := authmw.EncodeToken(jwt, &c)
	require.NoError(t, err)
	return tok
}

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(b)
}
