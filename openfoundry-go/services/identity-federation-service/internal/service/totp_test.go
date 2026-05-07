package service_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

func TestEnrollmentShape(t *testing.T) {
	t.Parallel()
	e, err := service.CreateEnrollment("alice@example.com")
	require.NoError(t, err)
	// Secret: 20 bytes → ~32 base32 chars (no padding).
	assert.GreaterOrEqual(t, len(e.Secret), 30)
	assert.Equal(t, strings.ToUpper(e.Secret), e.Secret, "base32 RFC4648 alphabet is uppercase")
	assert.Len(t, e.RecoveryCodes, 8)
	for _, c := range e.RecoveryCodes {
		assert.Equal(t, strings.ToUpper(c), c, "recovery codes are uppercase")
		assert.LessOrEqual(t, len(c), 10)
		assert.GreaterOrEqual(t, len(c), 4)
	}
	// otpauth URI shape pinned: must carry secret + algo + digits + period.
	assert.Contains(t, e.OTPAuthURI, "otpauth://totp/OpenFoundry:alice@example.com")
	assert.Contains(t, e.OTPAuthURI, "secret="+e.Secret)
	assert.Contains(t, e.OTPAuthURI, "algorithm=SHA1")
	assert.Contains(t, e.OTPAuthURI, "digits=6")
	assert.Contains(t, e.OTPAuthURI, "period=30")
}

func TestVerifyTOTPRejectsRandomCodes(t *testing.T) {
	t.Parallel()
	e, err := service.CreateEnrollment("a@b")
	require.NoError(t, err)
	assert.False(t, service.VerifyTOTP(e.Secret, "000000"),
		"a random 6-digit code is statistically very unlikely to be valid")
	assert.False(t, service.VerifyTOTP(e.Secret, "garbage"))
	assert.False(t, service.VerifyTOTP("not-base32!", "123456"))
}

func TestRecoveryCodeRoundTrip(t *testing.T) {
	t.Parallel()
	codes := []string{"AAAA-AAAA", "BBBB-BBBB", "CCCC-CCCC"}
	hashes := service.HashRecoveryCodes(codes)
	require.Len(t, hashes, 3)

	// Consume the second code.
	remaining, ok := service.ConsumeRecoveryCode(hashes, "BBBB-BBBB")
	require.True(t, ok)
	assert.Len(t, remaining, 2)
	assert.Equal(t, hashes[0], remaining[0])
	assert.Equal(t, hashes[2], remaining[1])

	// Same code again — no longer present.
	_, ok = service.ConsumeRecoveryCode(remaining, "BBBB-BBBB")
	assert.False(t, ok)

	// Unknown code.
	_, ok = service.ConsumeRecoveryCode(hashes, "ZZZZ-ZZZZ")
	assert.False(t, ok)
}

func TestHashTokenIsURLSafeBase64NoPadding(t *testing.T) {
	t.Parallel()
	h := service.HashToken("alice")
	// 32-byte sha-256 → 43 chars URL-safe base64 no padding
	assert.Len(t, h, 43)
	assert.NotContains(t, h, "=", "no padding")
	assert.NotContains(t, h, "+", "URL-safe alphabet")
	assert.NotContains(t, h, "/", "URL-safe alphabet")
}
