package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

type mdMap map[string]string

func (m mdMap) Get(k string) string { return m[k] }

func TestAuthenticateMissingHeaderRejected(t *testing.T) {
	t.Parallel()
	a := NewAuthenticator("test-secret", false)
	_, err := a.Authenticate(mdMap{})
	if !IsUnauthenticated(err) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
}

func TestAuthenticateAnonymousFallback(t *testing.T) {
	t.Parallel()
	a := NewAuthenticator("test-secret", true)
	req, err := a.Authenticate(mdMap{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if req != nil {
		t.Fatalf("expected nil AuthenticatedRequest in anonymous mode, got %+v", req)
	}
}

func TestAuthenticateBearerSchemeRequired(t *testing.T) {
	t.Parallel()
	a := NewAuthenticator("test-secret", false)
	_, err := a.Authenticate(mdMap{"authorization": "Token abc"})
	if !IsUnauthenticated(err) {
		t.Fatalf("non-Bearer scheme must be rejected: %v", err)
	}
}

func TestAuthenticateValidJWT(t *testing.T) {
	t.Parallel()
	cfg := authmw.NewJWTConfig("test-secret")
	claims := &authmw.Claims{
		Sub:   uuid.New(),
		IAT:   time.Now().Unix(),
		EXP:   time.Now().Add(time.Hour).Unix(),
		JTI:   uuid.New(),
		Email: "user@example.com",
		Name:  "Tester",
		Roles: []string{"viewer"},
	}
	tok, err := authmw.EncodeToken(cfg, claims)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	a := NewAuthenticator("test-secret", false)
	req, err := a.Authenticate(mdMap{"authorization": "Bearer " + tok})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if req == nil || req.Claims.Email != "user@example.com" {
		t.Fatalf("unexpected req: %+v", req)
	}
}

func TestAnonymousDefaultQuotasMatchesStandardTier(t *testing.T) {
	t.Parallel()
	if AnonymousDefaultQuotas().MaxRows != authmw.QuotaStandard().MaxQueryLimit {
		t.Fatalf("anonymous quotas must equal standard tier")
	}
}

func TestHeaderMetadataCaseInsensitive(t *testing.T) {
	t.Parallel()
	h := HeaderMetadata{"Authorization": []string{"Bearer x"}}
	if h.Get("authorization") != "Bearer x" {
		t.Fatalf("case-insensitive lookup failed: %q", h.Get("authorization"))
	}
}
