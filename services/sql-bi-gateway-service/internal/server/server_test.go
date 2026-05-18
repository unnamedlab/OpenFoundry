package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/server"
)

func TestHealthzAnonymous(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "sql-bi-gateway-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret"
	cfg.AllowAnonymous = false

	router := server.BuildRouter(cfg, server.Deps{Metrics: observability.NewMetrics()})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz status: %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("/healthz body: %q", rec.Body.String())
	}
}

func TestAPIv1RejectsMissingJWT(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "sql-bi-gateway-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret"
	cfg.AllowAnonymous = false

	router := server.BuildRouter(cfg, server.Deps{Metrics: observability.NewMetrics()})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/queries/saved/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rec.Code)
	}
}

func TestAPIv1AllowsAnonymousWhenConfigured(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "sql-bi-gateway-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret"
	cfg.AllowAnonymous = true

	router := server.BuildRouter(cfg, server.Deps{Metrics: observability.NewMetrics()})

	// no Pool wired → stub-only response, but 200.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/queries/saved/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsAnonymous(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "sql-bi-gateway-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret"

	router := server.BuildRouter(cfg, server.Deps{Metrics: observability.NewMetrics()})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status: %d", rec.Code)
	}
}
