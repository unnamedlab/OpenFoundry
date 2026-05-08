package domain

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialKeyDerivationMatchesRustGolden(t *testing.T) {
	key, err := DeriveCredentialKey("", "openfoundry-dev-secret")
	require.NoError(t, err)
	require.Equal(t, "b9d3ee554c1575e6bf4cf575de873ac91f9fc7e12046b4dfdea0df6b24632fb4", hex.EncodeToString(key[:]))

	raw := []byte("0123456789abcdef0123456789abcdef")
	key, err = DeriveCredentialKey(base64.StdEncoding.EncodeToString(raw), "ignored")
	require.NoError(t, err)
	require.Equal(t, raw, key[:])
}

func TestCredentialDecryptReadsRustLayoutGolden(t *testing.T) {
	var key [32]byte
	raw, err := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	require.NoError(t, err)
	copy(key[:], raw)
	blob, err := hex.DecodeString("01101112131415161718191a1b0e8be8733be449d6a9076d69ae0a25978f6dd7fe0d90456a6c70f9c8")
	require.NoError(t, err)
	plain, err := DecryptCredential(key, blob)
	require.NoError(t, err)
	require.Equal(t, []byte("super-secret"), plain)
}

func TestCredentialEncryptUsesRustLayoutAndRoundTrips(t *testing.T) {
	key, err := DeriveCredentialKey("", "openfoundry-dev-secret")
	require.NoError(t, err)
	blob, err := EncryptCredential(key, []byte("super-secret"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(blob), 1+credentialNonceLen)
	require.Equal(t, byte(1), blob[0])
	plain, err := DecryptCredential(key, blob)
	require.NoError(t, err)
	require.Equal(t, []byte("super-secret"), plain)
}

func TestCredentialDecryptErrorsMatchRust(t *testing.T) {
	key, err := DeriveCredentialKey("", "x")
	require.NoError(t, err)
	_, err = DecryptCredential(key, []byte{1, 2, 3})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ciphertext blob is malformed (len=3)")

	blob, err := EncryptCredential(key, []byte("hi"))
	require.NoError(t, err)
	blob[0] = 9
	_, err = DecryptCredential(key, blob)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "unsupported credential version: 9"))
}
