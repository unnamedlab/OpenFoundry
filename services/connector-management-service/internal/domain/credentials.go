package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const (
	credentialVersion  byte = 1
	credentialNonceLen      = 12
)

type CredentialCryptoError struct {
	Kind    string
	Message string
}

func (e *CredentialCryptoError) Error() string { return e.Message }

// DeriveCredentialKey mirrors Rust credential_crypto::derive_key.
func DeriveCredentialKey(envKeyB64, jwtSecret string) ([32]byte, error) {
	var out [32]byte
	if strings.TrimSpace(envKeyB64) != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKeyB64))
		if err != nil {
			return out, &CredentialCryptoError{Kind: "encrypt", Message: fmt.Sprintf("encryption failed: base64 decode key: %v", err)}
		}
		if len(raw) != 32 {
			return out, &CredentialCryptoError{Kind: "bad_key_length", Message: fmt.Sprintf("CREDENTIAL_ENCRYPTION_KEY must decode to 32 bytes (got %d)", len(raw))}
		}
		copy(out[:], raw)
		return out, nil
	}
	digest := sha256.Sum256([]byte("openfoundry/credential-encryption/v1\x00" + jwtSecret))
	copy(out[:], digest[:])
	return out, nil
}

func EncryptCredential(key [32]byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, &CredentialCryptoError{Kind: "encrypt", Message: fmt.Sprintf("encryption failed: %v", err)}
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, credentialNonceLen)
	if err != nil {
		return nil, &CredentialCryptoError{Kind: "encrypt", Message: fmt.Sprintf("encryption failed: %v", err)}
	}
	nonce := make([]byte, credentialNonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, &CredentialCryptoError{Kind: "encrypt", Message: fmt.Sprintf("encryption failed: %v", err)}
	}
	out := make([]byte, 0, 1+credentialNonceLen+len(plaintext)+gcm.Overhead())
	out = append(out, credentialVersion)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, nil), nil
}

func DecryptCredential(key [32]byte, blob []byte) ([]byte, error) {
	if len(blob) < 1+credentialNonceLen {
		return nil, &CredentialCryptoError{Kind: "malformed", Message: fmt.Sprintf("ciphertext blob is malformed (len=%d)", len(blob))}
	}
	if blob[0] != credentialVersion {
		return nil, &CredentialCryptoError{Kind: "unsupported_version", Message: fmt.Sprintf("unsupported credential version: %d", blob[0])}
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, &CredentialCryptoError{Kind: "decrypt", Message: fmt.Sprintf("decryption failed: %v", err)}
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, credentialNonceLen)
	if err != nil {
		return nil, &CredentialCryptoError{Kind: "decrypt", Message: fmt.Sprintf("decryption failed: %v", err)}
	}
	plain, err := gcm.Open(nil, blob[1:1+credentialNonceLen], blob[1+credentialNonceLen:], nil)
	if err != nil {
		return nil, &CredentialCryptoError{Kind: "decrypt", Message: fmt.Sprintf("decryption failed: %v", err)}
	}
	return plain, nil
}
