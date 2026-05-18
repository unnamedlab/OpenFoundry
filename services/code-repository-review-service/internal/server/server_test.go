package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/server"
)

func TestProtectedRoutesRequireBearerToken(t *testing.T) {
	srv := newTestServer(t, authmw.NewJWTConfig("code-repository-review-router-test-secret"))

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/global-branches/"},
		{http.MethodPost, "/v1/global-branches/"},
		{http.MethodGet, "/v1/global-branches/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/v1/global-branches/00000000-0000-0000-0000-000000000001/links"},
		{http.MethodGet, "/v1/global-branches/00000000-0000-0000-0000-000000000001/resources"},
		{http.MethodPost, "/v1/global-branches/00000000-0000-0000-0000-000000000001/promote"},
		{http.MethodPost, "/v1/code-security/scans"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			srv.Handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

func TestPublicRoutesStayPublic(t *testing.T) {
	srv := newTestServer(t, authmw.NewJWTConfig("code-repository-review-router-test-secret"))

	for _, path := range []string{"/healthz", "/healthz/json", "/metrics"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		srv.Handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, path)
	}
}

func TestProtectedRoutePassesAuthWithValidToken(t *testing.T) {
	jwt := authmw.NewJWTConfig("code-repository-review-router-test-secret")
	srv := newTestServer(t, jwt)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/global-branches/00000000-0000-0000-0000-000000000001", nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor(t, jwt))
	srv.Handler.ServeHTTP(rec, req)

	// Without a wired *repo.GlobalBranchRepo the handler nil-derefs, so
	// we only assert that the middleware let the request through — the
	// auth gate is the unit under test here.
	require.NotEqual(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func newTestServer(t *testing.T, jwt *authmw.JWTConfig) *http.Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "code-repository-review-service"
	cfg.Service.Version = "test"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	// Empty Handlers — auth is enforced before any handler body runs.
	return server.New(cfg, jwt, &handlers.Handlers{}, observability.NewMetrics())
}

func tokenFor(t *testing.T, cfg *authmw.JWTConfig) string {
	t.Helper()
	now := time.Now()
	accessUse := "access"
	tok, err := authmw.EncodeToken(cfg, &authmw.Claims{
		Sub:      uuid.New(),
		IAT:      now.Unix(),
		EXP:      now.Add(time.Hour).Unix(),
		JTI:      uuid.New(),
		Email:    "router-test@example.com",
		Name:     "Router Test",
		Roles:    []string{"admin"},
		TokenUse: &accessUse,
	})
	require.NoError(t, err)
	return tok
}
