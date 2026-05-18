package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/audit"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/kms"
)

// buildTestRouter mounts the cipher-service router with a tiny KMS +
// nil repo (good enough for the auth / capability tests below; the
// real wire-shape exercises live in handler/cipher_test.go).
func buildTestRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "cipher-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "test-secret-please-rotate"

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	k, err := kms.NewLocalKMS(kek, "local:test")
	if err != nil {
		t.Fatalf("NewLocalKMS: %v", err)
	}
	state := &handler.State{
		Repo:  nil, // unused on auth-rejection paths
		KMS:   k,
		Audit: audit.NewRecorder(nil, nil),
	}
	jwtCfg := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)
	return BuildRouter(cfg, state, nil, jwtCfg)
}

// TestMountAPIRoutes_RequiresAuth pins the gateway contract: every
// /api/v1/auth/cipher/* route returns a 401 (missing bearer token)
// rather than a 404, so the frontend keeps seeing a typed auth
// error and not a router miss.
func TestMountAPIRoutes_RequiresAuth(t *testing.T) {
	t.Parallel()
	r := buildTestRouter(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/auth/cipher/keys"},
		{http.MethodPost, "/api/v1/auth/cipher/keys"},
		{http.MethodGet, "/api/v1/auth/cipher/keys/00000000-0000-0000-0000-000000000000"},
		{http.MethodPost, "/api/v1/auth/cipher/keys/00000000-0000-0000-0000-000000000000/rotate"},
		{http.MethodPost, "/api/v1/auth/cipher/keys/00000000-0000-0000-0000-000000000000/retire"},
		{http.MethodPost, "/api/v1/auth/cipher/encrypt"},
		{http.MethodPost, "/api/v1/auth/cipher/decrypt"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s = %d, want 401 (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body=%q)", err, w.Body.String())
			}
			if body["error"] == "" {
				t.Fatalf("expected error envelope, got %+v", body)
			}
		})
	}
}

// TestHealthz remains public — gateway smoke probes hit it without a
// JWT, so this test guards against accidentally pulling /healthz
// under the authenticated mount.
func TestHealthz(t *testing.T) {
	t.Parallel()
	r := buildTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", w.Code)
	}
}
