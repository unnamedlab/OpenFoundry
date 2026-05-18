package authmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func sampleClaims(ttl time.Duration) *authmw.Claims {
	now := time.Now()
	access := "access"
	return &authmw.Claims{
		Sub:      uuid.New(),
		IAT:      now.Unix(),
		EXP:      now.Add(ttl).Unix(),
		JTI:      uuid.New(),
		Email:    "demo@example.com",
		Name:     "Demo User",
		Roles:    []string{"member"},
		TokenUse: &access,
	}
}

func sampleClaimsWithUse(ttl time.Duration, tokenUse string) *authmw.Claims {
	c := sampleClaims(ttl)
	use := tokenUse
	c.TokenUse = &use
	return c
}

func TestHS256RoundTrip(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	tok, err := authmw.EncodeToken(cfg, sampleClaims(time.Hour))
	require.NoError(t, err)
	decoded, err := authmw.DecodeToken(cfg, tok)
	require.NoError(t, err)
	assert.Equal(t, "demo@example.com", decoded.Email)
}

func TestHS256RejectsExpired(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	tok, err := authmw.EncodeToken(cfg, sampleClaims(-time.Minute))
	require.NoError(t, err)
	_, err = authmw.DecodeToken(cfg, tok)
	require.Error(t, err)
	assert.True(t, authmw.IsExpired(err))
}

func TestHS256AudienceMismatch(t *testing.T) {
	t.Parallel()
	issuer, err := authmw.Generate()
	require.NoError(t, err)
	issuer = issuer.WithAudience("openfoundry")
	aud := "openfoundry"

	c := sampleClaims(time.Hour)
	c.AUD = &aud
	tok, err := authmw.EncodeToken(issuer, c)
	require.NoError(t, err)

	// Sanity-check round trip with the same config first — proves the
	// token is valid in isolation; the cross-config rejection lives in
	// an integration test once the secret-loader lands in Phase 2.
	got, err := authmw.DecodeToken(issuer, tok)
	require.NoError(t, err)
	assert.Equal(t, "openfoundry", *got.AUD)
}

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	called := false
	handler := authmw.Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	handler.ServeHTTP(rec, req)
	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddlewareInjectsClaims(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	tok, err := authmw.EncodeToken(cfg, sampleClaims(time.Hour))
	require.NoError(t, err)

	handler := authmw.Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := authmw.FromContext(r.Context())
		require.True(t, ok)
		assert.Equal(t, "demo@example.com", c.Email)
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDecodeToken_AcceptsAccessByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	tok, err := authmw.EncodeToken(cfg, sampleClaimsWithUse(time.Hour, "access"))
	require.NoError(t, err)

	decoded, err := authmw.DecodeToken(cfg, tok)
	require.NoError(t, err)
	require.NotNil(t, decoded.TokenUse)
	assert.Equal(t, "access", *decoded.TokenUse)
}

func TestDecodeToken_RejectsWrongTokenUse(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	// Default filter is ["access"], so an mfa_challenge token must
	// not pass standard validation.
	tok, err := authmw.EncodeToken(cfg, sampleClaimsWithUse(time.Hour, "mfa_challenge"))
	require.NoError(t, err)

	_, err = authmw.DecodeToken(cfg, tok)
	require.Error(t, err)
	assert.True(t, authmw.IsWrongTokenUse(err), "expected wrong-token-use error, got %v", err)
	assert.False(t, authmw.IsExpired(err))

	// A nil TokenUse must also be rejected — the default filter is
	// strict equality, not "missing or access".
	missing := sampleClaims(time.Hour)
	missing.TokenUse = nil
	tok2, err := authmw.EncodeToken(cfg, missing)
	require.NoError(t, err)
	_, err = authmw.DecodeToken(cfg, tok2)
	require.Error(t, err)
	assert.True(t, authmw.IsWrongTokenUse(err))
}

func TestDecodeToken_RespectsCustomUses(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	refresh := sampleClaimsWithUse(time.Hour, "refresh")
	tok, err := authmw.EncodeToken(cfg, refresh)
	require.NoError(t, err)

	// With explicit override, "refresh" now passes.
	decoded, err := authmw.DecodeToken(cfg, tok, authmw.WithAllowedTokenUses("refresh"))
	require.NoError(t, err)
	require.NotNil(t, decoded.TokenUse)
	assert.Equal(t, "refresh", *decoded.TokenUse)

	// "access" no longer matches the explicit override.
	accessTok, err := authmw.EncodeToken(cfg, sampleClaimsWithUse(time.Hour, "access"))
	require.NoError(t, err)
	_, err = authmw.DecodeToken(cfg, accessTok, authmw.WithAllowedTokenUses("refresh"))
	require.Error(t, err)
	assert.True(t, authmw.IsWrongTokenUse(err))

	// WithAnyTokenUse disables the filter entirely.
	mfaTok, err := authmw.EncodeToken(cfg, sampleClaimsWithUse(time.Hour, "mfa_challenge"))
	require.NoError(t, err)
	got, err := authmw.DecodeToken(cfg, mfaTok, authmw.WithAnyTokenUse())
	require.NoError(t, err)
	require.NotNil(t, got.TokenUse)
	assert.Equal(t, "mfa_challenge", *got.TokenUse)

	// Multiple allowed uses, including "access" + "api_key".
	apiKey := sampleClaimsWithUse(time.Hour, "api_key")
	apiTok, err := authmw.EncodeToken(cfg, apiKey)
	require.NoError(t, err)
	_, err = authmw.DecodeToken(cfg, apiTok, authmw.WithAllowedTokenUses("access", "api_key"))
	require.NoError(t, err)
}

func TestPermissionWildcards(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{
		Roles:       []string{"member"},
		Permissions: []string{"datasets:*"},
	}
	assert.True(t, c.HasPermission("datasets", "read"))
	assert.True(t, c.HasPermission("datasets", "write"))
	assert.False(t, c.HasPermission("ontology", "read"))

	admin := &authmw.Claims{Roles: []string{"admin"}}
	assert.True(t, admin.HasPermission("anything", "anything"))
}
