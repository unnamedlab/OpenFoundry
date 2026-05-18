package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/server"
)

func newSrv(t *testing.T) *http.Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "ontology-query-service"
	cfg.Service.Version = "test"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	jwt := authmw.NewJWTConfig("test-secret-key-not-for-prod")
	h := handlers.New(handlers.AppState{})
	m := observability.NewMetrics()
	return server.New(cfg, jwt, h, m)
}

func TestHealthzReturnsOK(t *testing.T) {
	t.Parallel()
	srv := newSrv(t)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ontology-query-service")
}

func TestMetricsExposedWithoutAuth(t *testing.T) {
	t.Parallel()
	srv := newSrv(t)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "# HELP")
}

func TestObjectReadRequiresAuth(t *testing.T) {
	t.Parallel()
	srv := newSrv(t)
	req := httptest.NewRequest("GET", "/api/v1/ontology/objects/00000000-0000-0000-0000-000000000000/00000000-0000-0000-0000-000000000000", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLinkRoutesAreMounted(t *testing.T) {
	t.Parallel()
	srv := newSrv(t)
	for _, path := range []string{
		"/api/v1/ontology/objects/00000000-0000-0000-0000-000000000000/00000000-0000-0000-0000-000000000000/links/ASSIGNED_TO/outgoing",
		"/api/v1/ontology/objects/00000000-0000-0000-0000-000000000000/00000000-0000-0000-0000-000000000000/links/ASSIGNED_TO/incoming",
	} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		// 401 (auth) proves the route exists; a 404 would mean an unmounted handler.
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "path=%s", path)
	}
}

func TestUnknownTopLevelRouteReturns404(t *testing.T) {
	t.Parallel()
	srv := newSrv(t)
	req := httptest.NewRequest("GET", "/totally-unknown", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
