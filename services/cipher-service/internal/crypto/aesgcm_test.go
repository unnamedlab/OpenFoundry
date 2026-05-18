package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

func mustDEK(t *testing.T) []byte {
	t.Helper()
	dek, err := NewDEK()
	if err != nil {
		t.Fatalf("NewDEK: %v", err)
	}
	if len(dek) != dekSize {
		t.Fatalf("DEK size = %d, want %d", len(dek), dekSize)
	}
	return dek
}

// TestRoundtrip pins the basic encrypt → decrypt path. The envelope
// must include the canonical prefix and decrypt back to the exact
// plaintext.
func TestRoundtrip(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	pt := []byte("the quick brown fox jumps over the lazy dog")

	env, err := Encrypt(keyID, 3, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if len(env) < HeaderSize {
		t.Fatalf("envelope too short: %d", len(env))
	}
	gotID, gotVersion, err := DecodeHeader(env)
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if gotID != keyID {
		t.Fatalf("key id mismatch: got %s want %s", gotID, keyID)
	}
	if gotVersion != 3 {
		t.Fatalf("version = %d, want 3", gotVersion)
	}

	dec, err := Decrypt(keyID, 3, dek, env)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, pt) {
		t.Fatalf("roundtrip mismatch: got %q want %q", dec, pt)
	}
}

// TestEncrypt_NoncesDiffer guards against accidental nonce reuse —
// the single failure mode that collapses AES-GCM confidentiality.
func TestEncrypt_NoncesDiffer(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	pt := []byte("identical plaintext")

	envA, err := Encrypt(keyID, 1, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt A: %v", err)
	}
	envB, err := Encrypt(keyID, 1, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt B: %v", err)
	}
	nonceA := envA[20:HeaderSize]
	nonceB := envB[20:HeaderSize]
	if bytes.Equal(nonceA, nonceB) {
		t.Fatal("two encryptions reused the same nonce — GCM is broken")
	}
	if bytes.Equal(envA, envB) {
		t.Fatal("envelopes match — encryption is deterministic, which is not allowed for AES-GCM")
	}
}

// TestDecrypt_TamperedCiphertext asserts that flipping a single
// ciphertext byte invalidates the GCM tag.
func TestDecrypt_TamperedCiphertext(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	env, err := Encrypt(keyID, 1, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip a byte in the ciphertext region (after the header).
	env[HeaderSize] ^= 0x01

	_, err = Decrypt(keyID, 1, dek, env)
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

// TestDecrypt_TamperedHeader asserts the GCM AAD binding catches a
// caller who rewrites the key_id/version prefix after the fact.
func TestDecrypt_TamperedHeader(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	env, err := Encrypt(keyID, 1, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Re-write version to 2 without re-encrypting.
	binary.BigEndian.PutUint32(env[16:20], 2)

	// DecodeHeader sees the tampered version, so we ask Decrypt for
	// what the wire now claims; the mismatch is caught by the AAD.
	_, err = Decrypt(keyID, 2, dek, env)
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope on AAD tamper, got %v", err)
	}
}

// TestDecrypt_WrongDEK asserts a different DEK can't open the envelope.
func TestDecrypt_WrongDEK(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	env, err := Encrypt(keyID, 1, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	otherDEK := make([]byte, dekSize)
	if _, err := rand.Read(otherDEK); err != nil {
		t.Fatalf("rand: %v", err)
	}
	_, err = Decrypt(keyID, 1, otherDEK, env)
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

// TestDecrypt_TruncatedEnvelope ensures short / empty inputs return a
// typed error instead of panicking.
func TestDecrypt_TruncatedEnvelope(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()

	for _, raw := range [][]byte{nil, {}, make([]byte, HeaderSize-1)} {
		_, err := Decrypt(keyID, 1, dek, raw)
		if !errors.Is(err, domain.ErrInvalidEnvelope) {
			t.Fatalf("truncated input must yield ErrInvalidEnvelope, got %v", err)
		}
	}

	_, _, err := DecodeHeader(make([]byte, HeaderSize-1))
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("DecodeHeader on short input must yield ErrInvalidEnvelope, got %v", err)
	}
}

// TestEncrypt_WrongDEKSize rejects callers that hand us a non-32-byte
// key.
func TestEncrypt_WrongDEKSize(t *testing.T) {
	t.Parallel()
	keyID := uuid.New()
	_, err := Encrypt(keyID, 1, make([]byte, 16), []byte("x"))
	if err == nil {
		t.Fatal("expected error on 16-byte DEK")
	}
}
