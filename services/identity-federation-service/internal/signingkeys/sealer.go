package signingkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrSealingKeyMissing signals the environment did not provide a key.
var ErrSealingKeyMissing = errors.New("JWT_SIGNING_SEALING_KEY is not set")

// Sealer wraps + unwraps a private-key PEM with AES-256-GCM using a
// 32-byte sealing key. The ciphertext layout is nonce || ciphertext;
// the GCM tag is appended by the cipher implementation.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealerFromEnv reads JWT_SIGNING_SEALING_KEY (32 bytes, hex
// encoded) and returns a ready-to-use Sealer. Missing / malformed
// values surface as typed errors so callers can decide whether to
// fail boot or fall back to a development-only path.
func NewSealerFromEnv() (*Sealer, error) {
	raw := strings.TrimSpace(os.Getenv("JWT_SIGNING_SEALING_KEY"))
	if raw == "" {
		return nil, ErrSealingKeyMissing
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode JWT_SIGNING_SEALING_KEY: %w", err)
	}
	return NewSealer(key)
}

// NewSealer builds a Sealer from raw key bytes. Accepts 16 / 24 / 32
// byte keys (AES-128 / 192 / 256).
func NewSealer(key []byte) (*Sealer, error) {
	switch len(key) {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("sealing key must be 16, 24 or 32 bytes (got %d)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext into nonce||ciphertext||tag.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal. Returns an error when the ciphertext is too
// short, truncated, or the tag fails to authenticate.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(sealed) < ns {
		return nil, errors.New("sealed payload too short")
	}
	nonce, ct := sealed[:ns], sealed[ns:]
	pt, err := s.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return pt, nil
}
