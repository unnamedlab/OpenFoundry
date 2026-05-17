package server

import (
	"encoding/json"
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
)

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(BuildRouter(newTestConfig(""), observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "retrieval-context-service", body["service"])
}

func TestNoRetrievalRoutesYet(t *testing.T) {
	// Wire-compat: Rust binary is `fn main(){}`. The /api/v1/* business
	// surface is the libs/ai-kernel handlers re-export, which mounts
	// alongside libs/ai-kernel-go/handlers in a follow-up slice. Until
	// then only the auth probe lives under /api/v1.
	t.Parallel()
	srv := httptest.NewServer(BuildRouter(newTestConfig(""), observability.NewMetrics()))
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
		// authmw runs before chi's NotFound, so unauthenticated requests
		// to non-existent protected paths return 401 — the missing-route
		// signal is the same: no business handler is mounted.
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "path %s should not be a mounted business route", path)
	}
}

func TestAPIV1RequiresAuth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(BuildRouter(newTestConfig("retrieval-context-test-secret"), observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/_authz_probe")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAPIV1AcceptsValidBearer(t *testing.T) {
	t.Parallel()
	const secret = "retrieval-context-test-secret"
	srv := httptest.NewServer(BuildRouter(newTestConfig(secret), observability.NewMetrics()))
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

func newTestConfig(secret string) *config.Config {
	cfg := &config.Config{}
	cfg.Service.Name = "retrieval-context-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = secret
	return cfg
}

func mintAccessToken(t *testing.T, secret string) string {
	t.Helper()
	jwt := authmw.NewJWTConfig(secret)
	now := time.Now()
	claims := &authmw.Claims{
		Sub:   uuid.New(),
		IAT:   now.Unix(),
		EXP:   now.Add(time.Hour).Unix(),
		JTI:   uuid.New(),
		Email: "retrieval-test@example.com",
		Name:  "Retrieval Test",
		Roles: []string{"user"},
	}
	tok, err := authmw.EncodeToken(jwt, claims)
	require.NoError(t, err)
	return tok
}
