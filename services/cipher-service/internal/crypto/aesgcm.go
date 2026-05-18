// Package crypto is the cipher-service's encrypt/decrypt primitive.
// It binds an unwrapped 32-byte DEK to the self-describing envelope
// layout the Foundry-Cipher checklist (CIP.3) requires.
//
// Envelope wire layout (schema_version=1): the service JSON-encodes
// {key_id, key_version, algorithm_id, nonce, ciphertext, auth_tag,
// schema_version}; nonce/ciphertext/auth_tag are base64 strings inside
// the JSON object. The HTTP layer then base64-encodes the whole JSON
// object into the single ciphertext string callers store.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

// dekSize is the only DEK length the cipher-service emits. Pinned
// here so a future migration to 256-bit XChaCha or 512-bit composite
// keys is an explicit code change rather than a silent acceptance.
const dekSize = 32

// nonceSize is AES-GCM's canonical 12-byte nonce. The standard library
// will accept other sizes via NewGCMWithNonceSize, but the cipher
// service's wire envelope pins 12.
const nonceSize = 12

// gcmTagSize is AES-GCM's 16-byte authentication tag size.
const gcmTagSize = 16

// EnvelopeSchemaVersion is the current self-describing envelope schema.
const EnvelopeSchemaVersion = 1

// HeaderSize is retained only as the minimum serialized envelope size
// guard for older tests and callers that treat DecodeHeader as a cheap
// malformed-input check. Schema v1 envelopes are JSON, not fixed-width.
const HeaderSize = 1

// Envelope is the JSON object that is base64-encoded by the HTTP API as
// one ciphertext string. KeyVersion is an OpenFoundry extension that
// keeps rotated keys self-describing while preserving CIP.3's required
// fields.
type Envelope struct {
	KeyID         string `json:"key_id"`
	KeyVersion    uint32 `json:"key_version"`
	AlgorithmID   string `json:"algorithm_id"`
	Nonce         string `json:"nonce"`
	Ciphertext    string `json:"ciphertext"`
	AuthTag       string `json:"auth_tag"`
	SchemaVersion uint16 `json:"schema_version"`
}

// NewDEK draws a fresh 32-byte data-encryption key from crypto/rand.
// Callers wrap the DEK with a KMS Wrap call before persisting.
func NewDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("cipher: read DEK: %w", err)
	}
	return dek, nil
}

// Encrypt wraps plaintext under dek and returns the JSON envelope bytes.
// The handler base64-encodes these bytes into the external ciphertext
// string. The nonce is freshly drawn for every AES-GCM encryption.
func Encrypt(keyID uuid.UUID, version uint32, algorithm domain.Algorithm, dek, plaintext []byte) ([]byte, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("cipher: DEK must be %d bytes (got %d)", dekSize, len(dek))
	}
	if algorithm != domain.AlgorithmAES256GCM && algorithm != domain.AlgorithmAES256SIV {
		return nil, domain.ErrAlgorithmUnsupported
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("cipher: aes init: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher: gcm init: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if algorithm == domain.AlgorithmAES256SIV {
		nonce = syntheticNonce(dek, additionalData(keyID, version, algorithm), plaintext)
	} else if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("cipher: read nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plaintext, additionalData(keyID, version, algorithm))
	ciphertext := sealed[:len(sealed)-gcmTagSize]
	tag := sealed[len(sealed)-gcmTagSize:]
	env := Envelope{
		KeyID:         keyID.String(),
		KeyVersion:    version,
		AlgorithmID:   string(algorithm),
		Nonce:         base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:    base64.StdEncoding.EncodeToString(ciphertext),
		AuthTag:       base64.StdEncoding.EncodeToString(tag),
		SchemaVersion: EnvelopeSchemaVersion,
	}
	out, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("cipher: marshal envelope: %w", err)
	}
	return out, nil
}

// DecodeHeader pulls the (key_id, key_version) tuple out of an envelope
// without touching the DEK. Used by the handler layer to look the key
// up before fetching wrapped material from the repo.
func DecodeHeader(envelope []byte) (uuid.UUID, uint32, error) {
	decoded, err := DecodeEnvelope(envelope)
	if err != nil {
		return uuid.UUID{}, 0, err
	}
	return decoded.KeyID, decoded.KeyVersion, nil
}

// DecodedEnvelope is the validated, binary form of Envelope.
type DecodedEnvelope struct {
	KeyID         uuid.UUID
	KeyVersion    uint32
	AlgorithmID   domain.Algorithm
	Nonce         []byte
	Ciphertext    []byte
	AuthTag       []byte
	SchemaVersion uint16
}

// DecodeEnvelope validates and decodes a schema-v1 JSON envelope.
func DecodeEnvelope(envelope []byte) (DecodedEnvelope, error) {
	if len(envelope) < HeaderSize {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	var raw Envelope
	if err := json.Unmarshal(envelope, &raw); err != nil {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	if raw.SchemaVersion != EnvelopeSchemaVersion || raw.AlgorithmID == "" {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	keyID, err := uuid.Parse(raw.KeyID)
	if err != nil || raw.KeyVersion == 0 {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	nonce, err := base64.StdEncoding.DecodeString(raw.Nonce)
	if err != nil || len(nonce) != nonceSize {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	ciphertext, err := base64.StdEncoding.DecodeString(raw.Ciphertext)
	if err != nil {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	tag, err := base64.StdEncoding.DecodeString(raw.AuthTag)
	if err != nil || len(tag) != gcmTagSize {
		return DecodedEnvelope{}, domain.ErrInvalidEnvelope
	}
	return DecodedEnvelope{
		KeyID:         keyID,
		KeyVersion:    raw.KeyVersion,
		AlgorithmID:   domain.Algorithm(raw.AlgorithmID),
		Nonce:         nonce,
		Ciphertext:    ciphertext,
		AuthTag:       tag,
		SchemaVersion: raw.SchemaVersion,
	}, nil
}

// Decrypt reverses Encrypt. expectedKeyID, expectedVersion, and
// expectedAlgorithm guard against a caller swapping envelope metadata.
func Decrypt(expectedKeyID uuid.UUID, expectedVersion uint32, expectedAlgorithm domain.Algorithm, dek, envelope []byte) ([]byte, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("cipher: DEK must be %d bytes (got %d)", dekSize, len(dek))
	}
	decoded, err := DecodeEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	if decoded.KeyID != expectedKeyID || decoded.KeyVersion != expectedVersion || decoded.AlgorithmID != expectedAlgorithm {
		return nil, domain.ErrInvalidEnvelope
	}
	if decoded.AlgorithmID != domain.AlgorithmAES256GCM && decoded.AlgorithmID != domain.AlgorithmAES256SIV {
		return nil, domain.ErrAlgorithmUnsupported
	}

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("cipher: aes init: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher: gcm init: %w", err)
	}
	sealed := make([]byte, 0, len(decoded.Ciphertext)+len(decoded.AuthTag))
	sealed = append(sealed, decoded.Ciphertext...)
	sealed = append(sealed, decoded.AuthTag...)
	pt, err := aead.Open(nil, decoded.Nonce, sealed, additionalData(decoded.KeyID, decoded.KeyVersion, decoded.AlgorithmID))
	if err != nil {
		return nil, domain.ErrInvalidEnvelope
	}
	return pt, nil
}

func additionalData(keyID uuid.UUID, version uint32, algorithm domain.Algorithm) []byte {
	return []byte(fmt.Sprintf("schema=%d;key_id=%s;key_version=%d;algorithm_id=%s", EnvelopeSchemaVersion, keyID.String(), version, algorithm))
}

func syntheticNonce(dek, aad, plaintext []byte) []byte {
	mac := hmac.New(sha256.New, dek)
	mac.Write([]byte("openfoundry-cipher-aes-siv-v1"))
	mac.Write(aad)
	mac.Write(plaintext)
	sum := mac.Sum(nil)
	nonce := make([]byte, nonceSize)
	copy(nonce, sum[:nonceSize])
	return nonce
}
