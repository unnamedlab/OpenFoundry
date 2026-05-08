// Package jwksrotation ports services/identity-federation-service/
// src/hardening/jwks_rotation.rs (S3.1.c) — JWKS publication,
// Vault-backed rotation and rollback.
//
// Scope of this slice (P3.7.2.1):
//   - Domain types (VaultKeyRef, JwksKeyStatus, JwksKeyRecord,
//     PublicKeyEntry, JwkEntry, Jwks, RotationOutcome,
//     RollbackOutcome, SignedDigest, RotationPolicy).
//   - Errors (JwksRotationError + classification helpers,
//     SignError sentinel — full HTTP-bound vault_signer port lands
//     in P3.7.2.3).
//   - Two interfaces (TransitKeyClient, JwksKeyStore) consumed by
//     the orchestrator.
//   - In-memory test fake (InMemoryJwksKeyStore +
//     FakeTransitKeyClient).
//   - JwksRotationService orchestrator with rotate / rollback /
//     publish / sign_active.
//   - Pure builders (StableKid, BuildJwks, BuildJwksFromRecords).
//
// Out of scope (lands later):
//   - PostgresJwksKeyStore — network bound, lands with the consuming
//     service handler.
//   - VaultTransitSigner full HTTP client (~750 LOC) — lands in
//     P3.7.2.3 after the orchestrator's contract is solid.
//   - HTTP handlers (security_ops.rs) — lands in P3.7.2.4.
package jwksrotation

import (
	"errors"
	"fmt"
	"time"
)

// ─── Vault key reference ─────────────────────────────────────────────

// VaultKeyRef is the signing-key identifier as held in Vault transit
// (`transit/keys/<name>` + version). Mirrors struct VaultKeyRef.
type VaultKeyRef struct {
	Name    string `json:"name"`
	Version uint32 `json:"version"`
}

// ─── Rotation policy ─────────────────────────────────────────────────

// RotationPolicy bundles ASVS-L2-derived knobs that drive when a
// signing key must rotate and when its predecessor leaves the JWKS.
// Mirrors struct RotationPolicy.
type RotationPolicy struct {
	// ActiveDays is how long a key stays the sole publication
	// before a successor is created.
	ActiveDays int64
	// GraceDays is how long the previous key remains in the JWKS
	// after rotation, supporting in-flight tokens.
	GraceDays int64
}

// ASVSL2Default mirrors RotationPolicy::ASVS_L2_DEFAULT — 90 d
// active + 14 d grace. The NIST recommendation for JWS signing
// keys at OWASP ASVS L2.
var ASVSL2Default = RotationPolicy{ActiveDays: 90, GraceDays: 14}

// RotateAndRetire returns (rotate_at, retire_at) for a key that
// activated at `activatedAt`. rotate_at is when a successor must be
// published; retire_at is when the previous key is removed from
// JWKS.
func (p RotationPolicy) RotateAndRetire(activatedAt time.Time) (time.Time, time.Time) {
	rotateAt := activatedAt.Add(time.Duration(p.ActiveDays) * 24 * time.Hour)
	retireAt := rotateAt.Add(time.Duration(p.GraceDays) * 24 * time.Hour)
	return rotateAt, retireAt
}

// IsInGrace reports whether `now` falls inside the dual-publication
// window where both the previous and the current key must appear
// in JWKS.
func (p RotationPolicy) IsInGrace(prevActivatedAt, now time.Time) bool {
	rotateAt, retireAt := p.RotateAndRetire(prevActivatedAt)
	return !now.Before(rotateAt) && now.Before(retireAt)
}

// ─── JWKS key statuses + records ─────────────────────────────────────

// JwksKeyStatus mirrors the Rust enum JwksKeyStatus with serde
// snake_case wire form.
type JwksKeyStatus string

const (
	StatusActive  JwksKeyStatus = "active"
	StatusGrace   JwksKeyStatus = "grace"
	StatusRetired JwksKeyStatus = "retired"
)

// ParseJwksKeyStatus is the inverse of the JwksKeyStatus wire form.
// Unknown strings return a Store-classified JwksRotationError so
// schema drift between the writer and the reader surfaces loudly.
func ParseJwksKeyStatus(value string) (JwksKeyStatus, error) {
	switch JwksKeyStatus(value) {
	case StatusActive, StatusGrace, StatusRetired:
		return JwksKeyStatus(value), nil
	}
	return "", &JwksRotationError{Kind: ErrJwksStore, Message: "unknown JWKS key status " + value}
}

// PublicKeyEntry is the operator-facing projection of a published
// signing key. Mirrors struct PublicKeyEntry.
type PublicKeyEntry struct {
	Kid          string    `json:"kid"`
	Kty          string    `json:"kty"`
	PublicPEM    string    `json:"public_pem"`
	ActivatedAt  time.Time `json:"activated_at"`
}

// JwksKeyRecord is the canonical row stored in the runtime database.
// Mirrors struct JwksKeyRecord.
type JwksKeyRecord struct {
	Kid             string        `json:"kid"`
	Kty             string        `json:"kty"`
	PublicPEM       string        `json:"public_pem"`
	VaultKeyName    string        `json:"vault_key_name"`
	VaultKeyVersion uint32        `json:"vault_key_version"`
	Status          JwksKeyStatus `json:"status"`
	ActivatedAt     time.Time     `json:"activated_at"`
	GraceStartedAt  *time.Time    `json:"grace_started_at,omitempty"`
	RetireAfter     *time.Time    `json:"retire_after,omitempty"`
	RetiredAt       *time.Time    `json:"retired_at,omitempty"`
}

// VaultKeyRef returns the embedded Vault key reference. Mirrors fn
// vault_key_ref.
func (r *JwksKeyRecord) VaultKeyRef() VaultKeyRef {
	return VaultKeyRef{Name: r.VaultKeyName, Version: r.VaultKeyVersion}
}

// ─── JWKS publication ────────────────────────────────────────────────

// Jwks is the wire shape of the /.well-known/jwks.json document.
type Jwks struct {
	Keys []JwkEntry `json:"keys"`
}

// JwkEntry is one entry in the published JWKS array.
type JwkEntry struct {
	Kid       string `json:"kid"`
	Kty       string `json:"kty"`
	PublicPEM string `json:"public_pem"`
	// Use is the JWK "use" field — always "sig" for keys this
	// service publishes (signing-only; verification is JWKS-driven).
	Use string `json:"use"`
	// Status is non-standard but useful in audit/logs:
	// "active" or "grace".
	Status string `json:"status"`
}

// ─── Rotation outcomes ───────────────────────────────────────────────

// RotationOutcome describes the result of a successful Rotate call.
type RotationOutcome struct {
	PreviousActiveKid string    `json:"previous_active_kid"`
	ActiveKid         string    `json:"active_kid"`
	GraceUntil        time.Time `json:"grace_until"`
}

// RollbackOutcome describes the result of a successful Rollback.
type RollbackOutcome struct {
	RestoredActiveKid string    `json:"restored_active_kid"`
	DemotedKid        string    `json:"demoted_kid"`
	GraceUntil        time.Time `json:"grace_until"`
}

// SignedDigest carries the kid + key + raw signature bytes returned
// by SignActive. The wire form for the signature is left to the
// caller (typically base64-encoded inside a JWS).
type SignedDigest struct {
	Kid       string      `json:"kid"`
	Key       VaultKeyRef `json:"key"`
	Signature []byte      `json:"signature"`
}

// ─── Errors ──────────────────────────────────────────────────────────

// JwksRotationErrorKind discriminates JwksRotationError variants.
type JwksRotationErrorKind uint8

const (
	// ErrJwksStore wraps a database / store-side failure.
	ErrJwksStore JwksRotationErrorKind = iota + 1
	// ErrJwksState signals an inconsistent persisted state (e.g.
	// "no active JWKS key", "rollback target not in grace").
	ErrJwksState
	// ErrJwksVault wraps a Vault transit failure (signer-level).
	ErrJwksVault
)

// JwksRotationError is the typed error returned by the rotation
// surface. Callers use the Is* helpers (or errors.As) to
// discriminate.
type JwksRotationError struct {
	Kind    JwksRotationErrorKind
	Message string
	Wrapped error
}

func (e *JwksRotationError) Error() string {
	switch e.Kind {
	case ErrJwksStore:
		return "jwks store error: " + e.Message
	case ErrJwksState:
		return "jwks key state error: " + e.Message
	case ErrJwksVault:
		return "vault transit error: " + e.Message
	}
	return "jwks rotation error: " + e.Message
}

func (e *JwksRotationError) Unwrap() error { return e.Wrapped }

// IsJwksStore reports whether err is an ErrJwksStore JwksRotationError.
func IsJwksStore(err error) bool {
	var e *JwksRotationError
	return errors.As(err, &e) && e.Kind == ErrJwksStore
}

// IsJwksState reports whether err is an ErrJwksState JwksRotationError.
func IsJwksState(err error) bool {
	var e *JwksRotationError
	return errors.As(err, &e) && e.Kind == ErrJwksState
}

// IsJwksVault reports whether err is an ErrJwksVault JwksRotationError.
func IsJwksVault(err error) bool {
	var e *JwksRotationError
	return errors.As(err, &e) && e.Kind == ErrJwksVault
}

// jwksStoreErr wraps an error from any JwksKeyStore.* call.
func jwksStoreErr(cause error) error {
	if cause == nil {
		return nil
	}
	if _, ok := cause.(*JwksRotationError); ok {
		return cause // preserve already-typed errors
	}
	return &JwksRotationError{Kind: ErrJwksStore, Message: cause.Error(), Wrapped: cause}
}

// jwksVaultErr wraps an error from any TransitKeyClient.* call.
func jwksVaultErr(cause error) error {
	if cause == nil {
		return nil
	}
	if _, ok := cause.(*JwksRotationError); ok {
		return cause
	}
	return &JwksRotationError{Kind: ErrJwksVault, Message: cause.Error(), Wrapped: cause}
}

// jwksStateErr is a constructor for the State variant. Format-only
// helper; no wrapping.
func jwksStateErr(format string, args ...any) error {
	return &JwksRotationError{Kind: ErrJwksState, Message: fmt.Sprintf(format, args...)}
}

// SignError is the canonical Vault-side error returned by
// TransitKeyClient implementations. The full impl with HTTP retries
// + Vault Auth flow lands in P3.7.2.3; this slice exposes the type
// so the orchestrator + interface contract are stable.
type SignError struct {
	Reason string
	// Wrapped carries the underlying transport / decode error
	// when the implementation has one to surface.
	Wrapped error
}

func (e *SignError) Error() string { return "vault sign error: " + e.Reason }
func (e *SignError) Unwrap() error { return e.Wrapped }

// NewSignError constructs a SignError with the given reason.
func NewSignError(reason string) *SignError { return &SignError{Reason: reason} }

// WrapSignError wraps a transport / Vault error with a SignError.
func WrapSignError(reason string, wrapped error) *SignError {
	return &SignError{Reason: reason, Wrapped: wrapped}
}
