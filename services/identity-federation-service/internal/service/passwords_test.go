package service_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

func TestPasswordRoundTrip(t *testing.T) {
	t.Parallel()
	hash, err := service.HashPassword("correct horse battery staple")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$"),
		"PHC encoding starts with $argon2id$ — required for Rust ↔ Go compat")

	require.NoError(t, service.VerifyPassword("correct horse battery staple", hash))
	require.ErrorIs(t, service.VerifyPassword("nope", hash), service.ErrPasswordMismatch)
}

func TestPasswordRejectsMalformedHash(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, service.VerifyPassword("any", "not-a-phc"), service.ErrInvalidHash)
	require.ErrorIs(t, service.VerifyPassword("any", "$bcrypt$..."), service.ErrInvalidHash)
}

func TestRefreshTokenHashIsDeterministic(t *testing.T) {
	t.Parallel()
	a := service.HashRefreshToken("token-x")
	b := service.HashRefreshToken("token-x")
	c := service.HashRefreshToken("token-y")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Len(t, a, 64, "sha-256 hex digest is 64 chars")
}

func TestNewRefreshTokenIsHighEntropy(t *testing.T) {
	t.Parallel()
	a, err := service.NewRefreshTokenPlaintext()
	require.NoError(t, err)
	b, err := service.NewRefreshTokenPlaintext()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	// 32 bytes → 43 chars in raw URL-safe base64
	assert.GreaterOrEqual(t, len(a), 43)
}
