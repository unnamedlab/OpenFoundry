// Package domain holds the pure-Go cipher-service types and the
// lifecycle invariants that bind them. Nothing in this package may
// import internal/repo or internal/handler — these are the
// vocabulary the rest of the service speaks.
//
// Foundry-Cipher Milestone A (see
// docs/migration/foundry-cipher-1to1-checklist.md, items CIP.1–CIP.8)
// requires a credible authenticated-mode encrypt/decrypt path with a
// versioned, tenant-scoped key registry. The deterministic SIV mode
// (CIP.9) is intentionally out of scope here — the algorithm enum
// reserves the identifier so callers can declare intent at key
// creation, but encrypt/decrypt of SIV-tagged keys returns
// ErrAlgorithmUnsupported until Milestone B lands.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Algorithm names the authenticated-encryption primitive a key binds
// to. Stable string identifiers so the wire envelope (key_id || version
// || nonce || ct) and audit events round-trip across services.
type Algorithm string

const (
	// AlgorithmAES256GCM is the AEAD primitive the service currently
	// implements end-to-end. 12-byte nonce, 16-byte tag, non-deterministic.
	AlgorithmAES256GCM Algorithm = "AES_256_GCM"

	// AlgorithmAES256GCMSIV is reserved for Milestone B (CIP.9). Keys
	// declared as GCM-SIV persist correctly but encrypt/decrypt
	// returns ErrAlgorithmUnsupported until the deterministic path
	// lands.
	AlgorithmAES256GCMSIV Algorithm = "AES_256_GCM_SIV"
)

// Valid reports whether `a` is a known algorithm identifier.
func (a Algorithm) Valid() bool {
	switch a {
	case AlgorithmAES256GCM, AlgorithmAES256GCMSIV:
		return true
	}
	return false
}

// Status captures the lifecycle position of a key. Three states is the
// minimum that lets the cipher-service distinguish "encrypt + decrypt",
// "decrypt-only", and "no operations" — matches the public Foundry
// Cipher key-lifecycle contract (CIP.16, CIP.17 in the checklist).
type Status string

const (
	// StatusActive permits encrypt and decrypt.
	StatusActive Status = "active"

	// StatusRotating is an active key whose latest version has just
	// been rolled. Encrypt + decrypt both still succeed; the state is
	// surfaced so dashboards can show in-flight rotations.
	StatusRotating Status = "rotating"

	// StatusRetired refuses new encrypts but keeps decrypt available
	// so stored ciphertext stays readable. Reversible only by admins.
	StatusRetired Status = "retired"
)

// Valid reports whether `s` is one of the recognised lifecycle states.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusRotating, StatusRetired:
		return true
	}
	return false
}

// CipherKey is the tenant-scoped registry row. Every encrypt/decrypt
// envelope carries a key id that resolves to a CipherKey row plus the
// CipherKeyVersion the envelope references.
type CipherKey struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Alias     string
	Algorithm Algorithm
	// Version is the currently active version number (1-indexed). The
	// list of historical versions lives in CipherKeyVersion rows
	// keyed by KeyID.
	Version   uint32
	Status    Status
	CreatedAt time.Time
	// RotatedAt is the last time a new version was activated. nil
	// when the key has only the initial v1.
	RotatedAt *time.Time
}

// CipherKeyVersion is one wrapping of the DEK (data-encryption key)
// belonging to a CipherKey. The plaintext DEK is never persisted; only
// the result of KMS.Wrap(DEK) sits in WrappedKeyMaterial.
//
// A version is "active" when its row matches the parent key's Version.
// Older versions stay decryptable until the key is retired.
type CipherKeyVersion struct {
	KeyID              uuid.UUID
	Version            uint32
	WrappedKeyMaterial []byte
	// KMSKeyRef identifies the KEK that wrapped this DEK so a future
	// rotation of the KEK can rewrap without losing provenance.
	KMSKeyRef   string
	CreatedAt   time.Time
	ActivatedAt time.Time
	// RetiredAt marks the moment a version stopped being the active
	// one. Decrypt continues to work as long as the parent key is not
	// itself retired.
	RetiredAt *time.Time
}

// Sentinel errors. Handlers map these to HTTP status codes; the
// errors.Is contract lets the repo layer wrap them with context.
var (
	// ErrKeyNotFound is returned when the registry has no row for a
	// (tenant, key id) pair.
	ErrKeyNotFound = errors.New("cipher: key not found")

	// ErrKeyVersionNotFound is returned when the requested version is
	// missing — either it never existed or it was rotated out before
	// the row could be persisted.
	ErrKeyVersionNotFound = errors.New("cipher: key version not found")

	// ErrKeyRetired blocks encrypt operations against a retired key.
	// Decrypt callers see this only when the surrounding key is fully
	// retired (versions still resolve for older envelopes).
	ErrKeyRetired = errors.New("cipher: key retired")

	// ErrTenantMismatch is returned when a caller resolves a key from
	// a different tenant. Mapped to 404 to avoid leaking existence
	// across tenant boundaries.
	ErrTenantMismatch = errors.New("cipher: tenant mismatch")

	// ErrAlgorithmUnsupported is returned when a caller attempts an
	// encrypt/decrypt against a key whose algorithm has no
	// implementation yet (currently AES_256_GCM_SIV).
	ErrAlgorithmUnsupported = errors.New("cipher: algorithm not yet supported")

	// ErrInvalidEnvelope is returned by Decrypt when the on-wire
	// ciphertext is too short, malformed, or fails AEAD authentication.
	ErrInvalidEnvelope = errors.New("cipher: invalid envelope")
)

// CanEncrypt reports whether the key allows fresh encryptions in its
// current state.
func (k *CipherKey) CanEncrypt() bool {
	return k != nil && (k.Status == StatusActive || k.Status == StatusRotating)
}

// CanDecrypt reports whether the key still serves decrypt requests.
// Retired keys keep decrypt available so historical ciphertexts remain
// readable; a future Revoke transition (CIP.17, Milestone B) will be
// needed to hard-block decrypt.
func (k *CipherKey) CanDecrypt() bool {
	return k != nil && (k.Status == StatusActive || k.Status == StatusRotating || k.Status == StatusRetired)
}
