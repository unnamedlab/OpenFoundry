package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/server"
)

// TestRouter_HealthAndAuthGates is a smoke test: /healthz is public,
// /api/v1/functions is gated by libs/auth-middleware (401 without a
// bearer token).
func TestRouter_HealthAndAuthGates(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "function-runtime-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "test-secret-do-not-use"
	h := &handlers.Handlers{Store: repo.NewMemoryStore()}
	r := server.BuildRouter(cfg, h, nil)

	hw := httptest.NewRecorder()
	r.ServeHTTP(hw, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if hw.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", hw.Code)
	}

	for _, path := range []string{
		"/api/v1/functions",
		"/api/v1/functions/runs",
	} {
		aw := httptest.NewRecorder()
		r.ServeHTTP(aw, httptest.NewRequest(http.MethodGet, path, nil))
		if aw.Code != http.StatusUnauthorized {
			t.Fatalf("%s without auth: got %d, want 401", path, aw.Code)
		}
	}
}
