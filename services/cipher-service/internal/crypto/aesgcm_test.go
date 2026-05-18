package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
// must include the CIP.3 JSON fields and decrypt back to the exact
// plaintext.
func TestRoundtrip(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	pt := []byte("the quick brown fox jumps over the lazy dog")

	env, err := Encrypt(keyID, 3, domain.AlgorithmAES256GCM, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	decoded, err := DecodeEnvelope(env)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if decoded.KeyID != keyID || decoded.KeyVersion != 3 || decoded.AlgorithmID != domain.AlgorithmAES256GCM {
		t.Fatalf("decoded metadata mismatch: %+v", decoded)
	}
	if decoded.SchemaVersion != EnvelopeSchemaVersion || len(decoded.Nonce) != nonceSize || len(decoded.AuthTag) != gcmTagSize {
		t.Fatalf("decoded envelope shape mismatch: %+v", decoded)
	}
	gotID, gotVersion, err := DecodeHeader(env)
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if gotID != keyID || gotVersion != 3 {
		t.Fatalf("DecodeHeader = (%s, %d), want (%s, 3)", gotID, gotVersion, keyID)
	}

	dec, err := Decrypt(keyID, 3, domain.AlgorithmAES256GCM, dek, env)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, pt) {
		t.Fatalf("roundtrip mismatch: got %q want %q", dec, pt)
	}
}

func TestEnvelope_JSONShape(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	env, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(env, &raw); err != nil {
		t.Fatalf("envelope must be JSON: %v", err)
	}
	for _, field := range []string{"key_id", "key_version", "algorithm_id", "nonce", "ciphertext", "auth_tag", "schema_version"} {
		if _, ok := raw[field]; !ok {
			t.Fatalf("envelope missing %s: %s", field, string(env))
		}
	}
	for _, field := range []string{"nonce", "ciphertext", "auth_tag"} {
		if _, err := base64.StdEncoding.DecodeString(raw[field].(string)); err != nil {
			t.Fatalf("%s is not base64: %v", field, err)
		}
	}
}

// TestEncrypt_NoncesDiffer guards against accidental nonce reuse —
// the single failure mode that collapses AES-GCM confidentiality.

func TestAES256SIV_Deterministic(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	pt := []byte("join-key")

	envA, err := Encrypt(keyID, 1, domain.AlgorithmAES256SIV, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt A: %v", err)
	}
	envB, err := Encrypt(keyID, 1, domain.AlgorithmAES256SIV, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt B: %v", err)
	}
	if !bytes.Equal(envA, envB) {
		t.Fatalf("AES_256_SIV must be deterministic for same key/plaintext")
	}
	decoded, err := DecodeEnvelope(envA)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if decoded.AlgorithmID != domain.AlgorithmAES256SIV {
		t.Fatalf("algorithm = %s", decoded.AlgorithmID)
	}
	opened, err := Decrypt(keyID, 1, domain.AlgorithmAES256SIV, dek, envA)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(opened, pt) {
		t.Fatalf("opened = %q", opened)
	}
}

func TestEncrypt_NoncesDiffer(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	pt := []byte("identical plaintext")

	envA, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt A: %v", err)
	}
	envB, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, pt)
	if err != nil {
		t.Fatalf("Encrypt B: %v", err)
	}
	decA, err := DecodeEnvelope(envA)
	if err != nil {
		t.Fatalf("Decode A: %v", err)
	}
	decB, err := DecodeEnvelope(envB)
	if err != nil {
		t.Fatalf("Decode B: %v", err)
	}
	if bytes.Equal(decA.Nonce, decB.Nonce) {
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
	env, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	decoded, err := DecodeEnvelope(env)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	decoded.Ciphertext[0] ^= 0x01
	tampered := remarshalEnvelope(t, decoded)

	_, err = Decrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, tampered)
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

// TestDecrypt_TamperedHeader asserts the GCM AAD binding catches a
// caller who rewrites the key_version after the fact.
func TestDecrypt_TamperedHeader(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	env, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	decoded, err := DecodeEnvelope(env)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	decoded.KeyVersion = 2
	tampered := remarshalEnvelope(t, decoded)

	_, err = Decrypt(keyID, 2, domain.AlgorithmAES256GCM, dek, tampered)
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope on AAD tamper, got %v", err)
	}
}

// TestDecrypt_WrongDEK asserts a different DEK can't open the envelope.
func TestDecrypt_WrongDEK(t *testing.T) {
	t.Parallel()
	dek := mustDEK(t)
	keyID := uuid.New()
	env, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	otherDEK := make([]byte, dekSize)
	if _, err := rand.Read(otherDEK); err != nil {
		t.Fatalf("rand: %v", err)
	}
	_, err = Decrypt(keyID, 1, domain.AlgorithmAES256GCM, otherDEK, env)
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

	for _, raw := range [][]byte{nil, {}, []byte("{")} {
		_, err := Decrypt(keyID, 1, domain.AlgorithmAES256GCM, dek, raw)
		if !errors.Is(err, domain.ErrInvalidEnvelope) {
			t.Fatalf("truncated input must yield ErrInvalidEnvelope, got %v", err)
		}
	}

	_, _, err := DecodeHeader(nil)
	if !errors.Is(err, domain.ErrInvalidEnvelope) {
		t.Fatalf("DecodeHeader on short input must yield ErrInvalidEnvelope, got %v", err)
	}
}

// TestEncrypt_WrongDEKSize rejects callers that hand us a non-32-byte
// key.
func TestEncrypt_WrongDEKSize(t *testing.T) {
	t.Parallel()
	keyID := uuid.New()
	_, err := Encrypt(keyID, 1, domain.AlgorithmAES256GCM, make([]byte, 16), []byte("x"))
	if err == nil {
		t.Fatal("expected error on 16-byte DEK")
	}
}

func remarshalEnvelope(t *testing.T, decoded DecodedEnvelope) []byte {
	t.Helper()
	raw := Envelope{
		KeyID:         decoded.KeyID.String(),
		KeyVersion:    decoded.KeyVersion,
		AlgorithmID:   string(decoded.AlgorithmID),
		Nonce:         base64.StdEncoding.EncodeToString(decoded.Nonce),
		Ciphertext:    base64.StdEncoding.EncodeToString(decoded.Ciphertext),
		AuthTag:       base64.StdEncoding.EncodeToString(decoded.AuthTag),
		SchemaVersion: decoded.SchemaVersion,
	}
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return out
}
