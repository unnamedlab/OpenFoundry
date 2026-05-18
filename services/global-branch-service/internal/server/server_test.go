package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/config"
)

// TestBuildRouter_NoHandler_OnlyPublicRoutes confirms that the server
// stays bootable when no Handlers are wired (smoke mode without a
// configured DSN). Public surface — /healthz and the capability
// catalog — must respond; product routes must be absent from the
// catalog.
func TestBuildRouter_NoHandler_OnlyPublicRoutes(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "global-branch-service"
	cfg.Service.Version = "test"
	jwtCfg := authmw.NewJWTConfig("test-secret")

	r := BuildRouter(cfg, nil, nil, jwtCfg)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz status=%d want 200", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/_meta/capabilities", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/_meta/capabilities status=%d", w.Code)
	}
	var snap capabilities.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	for _, c := range snap.Capabilities {
		if c.ID == "global-branch.branches.create" {
			t.Fatalf("product routes should not be registered when h==nil; saw %s", c.ID)
		}
	}
}
