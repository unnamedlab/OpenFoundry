package presignclaim

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signClaimForTest mints a JWT directly from a Claim. Used by the
// expired-token test to bypass Sign's ttl<=0 → DefaultTTL clamp.
func signClaimForTest(t *testing.T, secret []byte, c Claim) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := tok.SignedString(secret)
	require.NoError(t, err)
	return signed
}

func TestSignerRejectsEmptySecret(t *testing.T) {
	t.Parallel()
	_, err := NewSigner(nil)
	require.Error(t, err)
	_, err = NewSigner([]byte{})
	require.Error(t, err)
}

func TestRoundTripSignAndVerify(t *testing.T) {
	t.Parallel()
	secret := []byte("super-secret-value")
	signer, err := NewSigner(secret)
	require.NoError(t, err)
	verifier, err := NewVerifier(secret)
	require.NoError(t, err)

	tok, err := signer.Sign("user-1", "ri.foundry.main.media_item.abc",
		[]string{"pii", "secret"}, time.Minute)
	require.NoError(t, err)
	assert.NotEmpty(t, tok)

	c, err := verifier.Verify(tok, "ri.foundry.main.media_item.abc")
	require.NoError(t, err)
	assert.Equal(t, "user-1", c.Sub)
	assert.Equal(t, []string{"pii", "secret"}, c.Markings)
	assert.True(t, c.EXP > c.IAT)
}

func TestVerifyRejectsItemRIDMismatch(t *testing.T) {
	t.Parallel()
	secret := []byte("secret")
	signer, _ := NewSigner(secret)
	verifier, _ := NewVerifier(secret)
	tok, _ := signer.Sign("u", "ri.foundry.main.media_item.A", nil, time.Minute)

	_, err := verifier.Verify(tok, "ri.foundry.main.media_item.B")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim targets")
}

func TestVerifyRejectsExpired(t *testing.T) {
	t.Parallel()
	secret := []byte("secret")
	verifier, _ := NewVerifier(secret)
	verifier.Leeway = 0 // disable leeway so expiry triggers immediately

	// Build an already-expired claim manually (Sign clamps ttl <= 0
	// to DefaultTTL, so we cannot mint a past-EXP token through it).
	now := time.Now().UTC()
	claim := Claim{
		Sub:     "u",
		ItemRID: "ri.foundry.main.media_item.X",
		IAT:     now.Add(-2 * time.Hour).Unix(),
		EXP:     now.Add(-time.Hour).Unix(),
	}
	tok := signClaimForTest(t, secret, claim)
	_, err := verifier.Verify(tok, "ri.foundry.main.media_item.X")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "expired")
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	t.Parallel()
	signer, _ := NewSigner([]byte("alpha"))
	verifier, _ := NewVerifier([]byte("beta"))
	tok, _ := signer.Sign("u", "ri.foundry.main.media_item.X", nil, time.Minute)
	_, err := verifier.Verify(tok, "ri.foundry.main.media_item.X")
	require.Error(t, err)
}

func TestSignDefaultTTLApplied(t *testing.T) {
	t.Parallel()
	signer, _ := NewSigner([]byte("secret"))
	verifier, _ := NewVerifier([]byte("secret"))
	tok, _ := signer.Sign("u", "ri.foundry.main.media_item.X", nil, 0)
	c, err := verifier.Verify(tok, "ri.foundry.main.media_item.X")
	require.NoError(t, err)
	delta := c.EXP - c.IAT
	assert.InDelta(t, int64(DefaultTTL.Seconds()), delta, 1)
}

func TestSignerCapTTL(t *testing.T) {
	t.Parallel()
	signer, _ := NewSigner([]byte("secret"))
	signer.CapTTL = 30 * time.Second
	verifier, _ := NewVerifier([]byte("secret"))
	tok, _ := signer.Sign("u", "ri.foundry.main.media_item.X", nil, 5*time.Minute)
	c, err := verifier.Verify(tok, "ri.foundry.main.media_item.X")
	require.NoError(t, err)
	delta := c.EXP - c.IAT
	assert.InDelta(t, int64(30), delta, 1, "CapTTL caps the requested 5 min to 30 s")
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	t.Parallel()
	signer, _ := NewSigner([]byte("secret"))
	verifier, _ := NewVerifier([]byte("secret"))
	tok, _ := signer.Sign("u", "ri.foundry.main.media_item.X", nil, time.Minute)
	tampered := tok[:len(tok)-2] + "AA"
	_, err := verifier.Verify(tampered, "ri.foundry.main.media_item.X")
	require.Error(t, err)
}
