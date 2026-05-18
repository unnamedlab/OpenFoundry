// Package crypto is the cipher-service's encrypt/decrypt primitive.
// It binds an unwrapped 32-byte DEK to the on-wire envelope layout the
// Foundry-Cipher checklist (CIP.3) requires.
//
// Envelope wire layout (Milestone A):
//
//	+----------------+----------------+----------------+-------------------+
//	| key_id (16 B)  | version (4 B)  | nonce (12 B)   | ciphertext || tag |
//	+----------------+----------------+----------------+-------------------+
//
// The four-byte version is big-endian. The trailing block is the
// AES-GCM ciphertext concatenated with the GCM tag (Go's stdlib emits
// them as a single byte slice). The envelope is intentionally
// self-describing so any reader with a fresh DEK lookup can decrypt
// without out-of-band metadata.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
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

// HeaderSize is the fixed-prefix length: key_id (16) + version (4) +
// nonce (12).
const HeaderSize = 16 + 4 + nonceSize

// NewDEK draws a fresh 32-byte data-encryption key from crypto/rand.
// Callers wrap the DEK with a KMS Wrap call before persisting.
func NewDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("cipher: read DEK: %w", err)
	}
	return dek, nil
}

// Encrypt wraps `plaintext` under `dek` and returns the full
// self-describing envelope. The DEK must be exactly 32 bytes;
// callers obtain it by KMS-unwrapping the stored CipherKeyVersion.
//
// The nonce is freshly drawn from crypto/rand on every call: re-using
// a nonce under the same DEK breaks GCM's confidentiality guarantee,
// so the function never accepts a caller-supplied nonce.
func Encrypt(keyID uuid.UUID, version uint32, dek, plaintext []byte) ([]byte, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("cipher: DEK must be %d bytes (got %d)", dekSize, len(dek))
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
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("cipher: read nonce: %w", err)
	}
	// AEAD additional data binds key_id+version so an attacker who
	// swaps the plaintext header (e.g. forges a different key id over
	// the same ciphertext) trips authentication.
	header := buildHeader(keyID, version, nonce)
	ct := aead.Seal(nil, nonce, plaintext, header[:HeaderSize-nonceSize])
	out := make([]byte, 0, HeaderSize+len(ct))
	out = append(out, header...)
	out = append(out, ct...)
	return out, nil
}

// DecodeHeader pulls the (key_id, version) tuple out of `envelope`
// without touching the DEK. Used by the handler layer to look the key
// up before fetching wrapped material from the repo.
func DecodeHeader(envelope []byte) (uuid.UUID, uint32, error) {
	if len(envelope) < HeaderSize {
		return uuid.UUID{}, 0, domain.ErrInvalidEnvelope
	}
	var id uuid.UUID
	copy(id[:], envelope[:16])
	version := binary.BigEndian.Uint32(envelope[16:20])
	return id, version, nil
}

// Decrypt reverses Encrypt. `expectedKeyID` and `expectedVersion`
// guard against a wire-tampering caller swapping the envelope header
// after the fact — both must match what DecodeHeader returned.
func Decrypt(expectedKeyID uuid.UUID, expectedVersion uint32, dek, envelope []byte) ([]byte, error) {
	if len(envelope) < HeaderSize {
		return nil, domain.ErrInvalidEnvelope
	}
	if len(dek) != dekSize {
		return nil, fmt.Errorf("cipher: DEK must be %d bytes (got %d)", dekSize, len(dek))
	}
	var gotID uuid.UUID
	copy(gotID[:], envelope[:16])
	gotVersion := binary.BigEndian.Uint32(envelope[16:20])
	if gotID != expectedKeyID || gotVersion != expectedVersion {
		return nil, domain.ErrInvalidEnvelope
	}
	nonce := envelope[20:HeaderSize]
	ct := envelope[HeaderSize:]

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("cipher: aes init: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher: gcm init: %w", err)
	}
	// Re-bind the (key_id, version) prefix as additional data; any
	// tampered prefix surfaces here as authentication failure.
	header := envelope[:HeaderSize-nonceSize]
	// aead.Open returns a generic error on tag mismatch / truncation.
	// We collapse every failure into ErrInvalidEnvelope so callers see
	// one typed error and timing analysis cannot tell the cases apart.
	pt, err := aead.Open(nil, nonce, ct, header)
	if err != nil {
		return nil, domain.ErrInvalidEnvelope
	}
	return pt, nil
}

// buildHeader writes the fixed prefix once so Encrypt/Decrypt agree on
// byte order. Always returns a HeaderSize-byte slice.
func buildHeader(keyID uuid.UUID, version uint32, nonce []byte) []byte {
	header := make([]byte, HeaderSize)
	copy(header[:16], keyID[:])
	binary.BigEndian.PutUint32(header[16:20], version)
	copy(header[20:], nonce)
	return header
}
