package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	aikernel "github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/handlers"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/config"
)

func ptr(s string) *string { return &s }
func token(t *testing.T, secret string) string {
	tok, err := authmw.EncodeToken(authmw.NewJWTConfig(secret), &authmw.Claims{Sub: uuid.New(), TokenUse: ptr("access"), EXP: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}
func TestKnowledgeRoutesNo501(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "knowledge"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.AllowFakeStore = true
	srv, err := New(cfg, observability.NewMetrics(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tok := token(t, "secret")
	req := httptest.NewRequest("GET", "/api/v1/ai/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code == http.StatusNotImplemented {
		t.Fatal("list returned 501")
	}
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := `{"name":"kb","description":"docs","embedding_provider_id":"00000000-0000-0000-0000-000000000001"}`
	req = httptest.NewRequest("POST", "/api/v1/ai/knowledge-bases", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code == 501 || w.Code >= 300 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProductionWithoutDatabaseFails(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "knowledge"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"

	_, err := New(cfg, observability.NewMetrics(), nil)
	if err == nil {
		t.Fatal("expected production wiring without database.url to fail")
	}
}

func TestFakeStoreRequiresExplicitAllowFlag(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "knowledge"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Database.URL = "postgres://example"

	_, err := New(cfg, observability.NewMetrics(), nil, WithKnowledgeStore(aikernel.NewFakeKnowledgeStore()))
	if err == nil {
		t.Fatal("expected injected fake store to require allow_fake_store=true")
	}
}
