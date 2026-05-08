package authmw_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func TestBuildAccessClaimsRoundTripsThroughEncodeDecode(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)
	cfg = cfg.WithIssuer("https://auth.test").WithAudience("openfoundry")

	orgID := uuid.New()
	claims := authmw.BuildAccessClaims(cfg, authmw.AccessClaimsInput{
		UserID:      uuid.New(),
		Email:       "demo@example.com",
		Name:        "Demo",
		Roles:       []string{"member"},
		Permissions: []string{"datasets:read"},
		OrgID:       &orgID,
		Attributes:  json.RawMessage(`{"region":"eu"}`),
		AuthMethods: []string{"password"},
	})
	require.NotNil(t, claims.TokenUse)
	assert.Equal(t, "access", *claims.TokenUse)
	require.NotNil(t, claims.ISS)
	assert.Equal(t, "https://auth.test", *claims.ISS)
	require.NotNil(t, claims.AUD)
	assert.Equal(t, "openfoundry", *claims.AUD)
	assert.NotEqual(t, uuid.Nil, claims.JTI)
	assert.Less(t, claims.IAT, claims.EXP)

	tok, err := authmw.EncodeToken(cfg, &claims)
	require.NoError(t, err)
	decoded, err := authmw.DecodeToken(cfg, tok)
	require.NoError(t, err)
	assert.Equal(t, "demo@example.com", decoded.Email)
}

func TestBuildRefreshClaimsHasRefreshTokenUseAndLongerTTL(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)
	cfg = cfg.WithAccessTTL(time.Hour).WithRefreshTTL(24 * time.Hour)

	c := authmw.BuildRefreshClaims(cfg, uuid.New())
	require.NotNil(t, c.TokenUse)
	assert.Equal(t, "refresh", *c.TokenUse)
	assert.Empty(t, c.Email)
	assert.Empty(t, c.Roles)
	// 24h refresh > 1h access (allowing a few seconds of jitter).
	assert.Greater(t, c.EXP-c.IAT, int64(60*60))
}

func TestBuildAPIKeyClaimsForcesAPIKeyTokenUseAndJTIEqualsKeyID(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)
	keyID := uuid.New()

	c := authmw.BuildAPIKeyClaims(cfg, authmw.APIKeyClaimsInput{
		UserID:    uuid.New(),
		Email:     "svc@example.com",
		Name:      "Service Account",
		Roles:     []string{"service"},
		APIKeyID:  keyID,
		ExpiresIn: 24 * time.Hour,
	})
	require.NotNil(t, c.TokenUse)
	assert.Equal(t, "api_key", *c.TokenUse)
	assert.Equal(t, []string{"api_key"}, c.AuthMethods)
	require.NotNil(t, c.APIKeyID)
	assert.Equal(t, keyID, *c.APIKeyID)
	assert.Equal(t, keyID, c.JTI, "JTI should track api_key_id for revocation")
}

func TestBuildAccessClaimsWithScopePropagatesSessionFields(t *testing.T) {
	t.Parallel()
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	scope := &authmw.SessionScope{AllowedMethods: []string{"GET"}}
	kind := "scoped_session"
	c := authmw.BuildAccessClaimsWithScope(
		cfg,
		authmw.AccessClaimsInput{UserID: uuid.New()},
		scope,
		&kind,
	)
	require.NotNil(t, c.SessionScope)
	assert.Equal(t, []string{"GET"}, c.SessionScope.AllowedMethods)
	require.NotNil(t, c.SessionKind)
	assert.Equal(t, "scoped_session", *c.SessionKind)
}
