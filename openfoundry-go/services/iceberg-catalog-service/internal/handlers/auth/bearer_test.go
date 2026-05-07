package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func newTestConfig() *Config {
	return &Config{
		Secret:              []byte("unit-test-secret"),
		JWTAudience:         "iceberg-catalog",
		JWTIssuer:           "foundry-iceberg",
		DefaultTokenTTLSecs: 3600,
		DefaultTenant:       "default",
	}
}

func TestExtractBearerLowercase(t *testing.T) {
	if v, ok := extractBearer("bearer abc"); !ok || v != "abc" {
		t.Fatalf("lowercase failed: %q ok=%v", v, ok)
	}
	if v, ok := extractBearer("Bearer xyz"); !ok || v != "xyz" {
		t.Fatalf("uppercase failed: %q ok=%v", v, ok)
	}
	if _, ok := extractBearer(""); ok {
		t.Fatalf("empty header should fail")
	}
	if _, ok := extractBearer("Basic abc"); ok {
		t.Fatalf("basic auth should fail")
	}
}

func TestEnforceForMethod(t *testing.T) {
	read := &AuthenticatedPrincipal{Scopes: map[string]struct{}{"api:iceberg-read": {}}}
	if err := read.EnforceForMethod(http.MethodGet); err != nil {
		t.Fatalf("read+GET failed: %v", err)
	}
	if err := read.EnforceForMethod(http.MethodPost); err == nil {
		t.Fatalf("read+POST must fail")
	}
	write := &AuthenticatedPrincipal{Scopes: map[string]struct{}{"api:iceberg-write": {}}}
	if err := write.EnforceForMethod(http.MethodPost); err != nil {
		t.Fatalf("write+POST failed: %v", err)
	}
	if err := write.EnforceForMethod(http.MethodGet); err != nil {
		t.Fatalf("write+GET failed: %v", err)
	}
	none := &AuthenticatedPrincipal{Scopes: map[string]struct{}{}}
	if err := none.EnforceForMethod(http.MethodGet); err == nil {
		t.Fatalf("no scope GET must fail")
	}
	if err := none.EnforceForMethod(http.MethodPost); err == nil {
		t.Fatalf("no scope POST must fail")
	}
}

type stubStore struct {
	tok *StoredAPIToken
	err error
}

func (s *stubStore) ValidateAPIToken(_ context.Context, _ string) (*StoredAPIToken, error) {
	return s.tok, s.err
}

func TestAuthenticateOftyToken(t *testing.T) {
	cfg := newTestConfig()
	uid := uuid.New()
	tid := uuid.New()
	store := &stubStore{tok: &StoredAPIToken{ID: tid, UserID: uid, Scopes: []string{"api:iceberg-read"}, Tenant: "default"}}
	p, err := Authenticate(context.Background(), "Bearer ofty_deadbeef", cfg, store)
	if err != nil {
		t.Fatalf("authenticate ofty: %v", err)
	}
	if p.Subject != uid.String() {
		t.Fatalf("subject mismatch: %q", p.Subject)
	}
	if _, ok := p.Scopes["api:iceberg-read"]; !ok {
		t.Fatalf("expected scope present")
	}
}

func TestAuthenticateJWT(t *testing.T) {
	cfg := newTestConfig()
	tok, err := IssueInternalJWT(cfg, "user-1", cfg.JWTIssuer, cfg.JWTAudience, []string{"api:iceberg-read"}, 60)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	p, err := Authenticate(context.Background(), "Bearer "+tok, cfg, nil)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if p.Subject != "user-1" {
		t.Fatalf("subject mismatch: %q", p.Subject)
	}
}

func TestAuthenticateRejectsAudienceMismatch(t *testing.T) {
	signing := newTestConfig()
	tok, err := IssueInternalJWT(signing, "user-1", signing.JWTIssuer, "wrong-aud", []string{"api:iceberg-read"}, 60)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	cfg := newTestConfig()
	_, err = Authenticate(context.Background(), "Bearer "+tok, cfg, nil)
	if err == nil {
		t.Fatalf("expected aud mismatch")
	}
	var unauth *ErrUnauthenticated
	if !errors.As(err, &unauth) {
		t.Fatalf("expected ErrUnauthenticated, got %v", err)
	}
}

func TestMiddlewareInjectsPrincipal(t *testing.T) {
	cfg := newTestConfig()
	tok, err := IssueInternalJWT(cfg, "user-1", cfg.JWTIssuer, cfg.JWTAudience, []string{"api:iceberg-read"}, 60)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	var sawSubject string
	handler := Middleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			t.Errorf("no principal")
		} else {
			sawSubject = p.Subject
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if sawSubject != "user-1" {
		t.Fatalf("subject not propagated: %q", sawSubject)
	}
}

func TestMiddlewareRejectsMissingHeader(t *testing.T) {
	cfg := newTestConfig()
	handler := Middleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "AuthenticationException") {
		t.Fatalf("expected exception type in body, got %s", rr.Body.String())
	}
}
