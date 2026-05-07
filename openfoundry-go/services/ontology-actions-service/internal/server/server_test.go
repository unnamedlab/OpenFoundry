package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/server"
)

const testJWTSecret = "ontology-actions-service-smoke-secret-do-not-use-in-prod"

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "ontology-actions-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = testJWTSecret
	return server.BuildRouter(cfg, nil, nil)
}

func devToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	cfg := authmw.NewJWTConfig(testJWTSecret)
	tok, err := authmw.EncodeToken(cfg, &authmw.Claims{
		Sub:   uuid.New(),
		IAT:   now.Unix(),
		EXP:   now.Add(time.Hour).Unix(),
		JTI:   uuid.New(),
		Email: "smoke@openfoundry.test",
		Name:  "Smoke Tester",
		Roles: []string{"ontology.editor"},
	})
	if err != nil {
		t.Fatalf("encode dev token: %v", err)
	}
	return tok
}

// Mirrors `list_action_types_requires_bearer_token` from
// `services/ontology-actions-service/tests/health.rs`.
func TestListActionTypesRequiresBearerToken(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)

	// 1. No token → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ontology/actions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(context.Background()))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// 2. Token → 200 + envelope.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ontology/actions", nil)
	req.Header.Set("Authorization", "Bearer "+devToken(t))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	data, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("expected `data` array, got %v", body)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty data array, got %d entries", len(data))
	}
	if total, _ := body["total"].(float64); total != 0 {
		t.Fatalf("expected total=0, got %v", body["total"])
	}
}

// Mirrors `absorbed_routes_require_bearer_token`.
func TestAbsorbedRoutesRequireBearerToken(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	for _, path := range []string{
		"/api/v1/ontology/funnel/sources",
		"/api/v1/ontology/storage/insights",
		"/api/v1/ontology/functions",
		"/api/v1/ontology/rules",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", path, rec.Code)
		}
	}
}

func TestHealthIsPublic(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	for _, path := range []string{"/health", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, rec.Code)
		}
	}
}
