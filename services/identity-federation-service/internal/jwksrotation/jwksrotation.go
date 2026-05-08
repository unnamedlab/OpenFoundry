package jwksrotation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ─── Interfaces ──────────────────────────────────────────────────────

// TransitKeyClient is the Vault-transit-shaped client the
// orchestrator delegates signing + key-management calls to.
// Mirrors trait TransitKeyClient.
//
// The full HTTP-bound implementation (VaultTransitSigner) lands in
// P3.7.2.3. This slice ships only the in-memory test fake.
type TransitKeyClient interface {
	// ConfiguredKeyRef returns the boot-time key reference (name +
	// version 1 by default; mirrors Rust fn configured_key_ref).
	ConfiguredKeyRef() VaultKeyRef
	// SignWithKey computes a signature over `digest` using the
	// given Vault key version. The hash + signature algorithms are
	// fixed to sha2-256 + pkcs1v15 in the production signer.
	SignWithKey(ctx context.Context, key VaultKeyRef, digest []byte) ([]byte, error)
	// LatestKeyRef returns the most recent (i.e. highest version)
	// key Vault has materialised for this client's configured
	// key name. Used during initial seeding.
	LatestKeyRef(ctx context.Context) (VaultKeyRef, error)
	// RotateKey asks Vault to bump the key version and returns the
	// new reference. Mirrors POST /transit/keys/<name>/rotate.
	RotateKey(ctx context.Context) (VaultKeyRef, error)
	// PublicKeyPEM returns the PEM-encoded public half of the key
	// + version. Mirrors GET /transit/keys/<name>.
	PublicKeyPEM(ctx context.Context, key VaultKeyRef) (string, error)
}

// JwksKeyStore is the persistence-shaped contract the orchestrator
// reads/writes against. Mirrors trait JwksKeyStore.
//
// The Postgres-backed implementation (PostgresJwksKeyStore) lands
// in P3.7.2.2. This slice ships the in-memory test fake.
type JwksKeyStore interface {
	// EnsureSchema is a startup-time call that materialises the
	// jwks_keys table + indexes. No-op for in-memory stores.
	EnsureSchema(ctx context.Context) error
	// ActiveKey returns the single row whose status is "active"
	// (or nil if the table has not been seeded yet).
	ActiveKey(ctx context.Context) (*JwksKeyRecord, error)
	// GraceKeys returns every "grace" row whose retire_after is
	// either NULL or strictly after `now`.
	GraceKeys(ctx context.Context, now time.Time) ([]JwksKeyRecord, error)
	// UpsertActiveSeed inserts the boot-time active row, or
	// rewrites the "active" row in place when one already exists
	// for the given kid. Mirrors the Rust ON CONFLICT (kid) DO
	// UPDATE shape.
	UpsertActiveSeed(ctx context.Context, record JwksKeyRecord) error
	// RotateTo demotes `previous` to grace (with retire_after =
	// graceUntil) and inserts/upgrades `next` as the new active
	// row. Atomic.
	RotateTo(ctx context.Context, previous JwksKeyRecord, next JwksKeyRecord, graceUntil time.Time) error
	// RollbackTo demotes `demoted` to grace (with retire_after =
	// graceUntil) and restores `restored` to active. Atomic.
	RollbackTo(ctx context.Context, restored JwksKeyRecord, demoted JwksKeyRecord, graceUntil time.Time) error
}

// ─── Service orchestrator ────────────────────────────────────────────

// Service drives the rotate/rollback/publish flow over a (store,
// transit) pair. Mirrors struct JwksRotationService.
type Service struct {
	store   JwksKeyStore
	transit TransitKeyClient
	policy  RotationPolicy
	kty     string
}

// NewService builds the orchestrator. `kty` defaults to "RSA"
// (matching the Rust hardcoded `kty: "RSA".to_string()`).
func NewService(store JwksKeyStore, transit TransitKeyClient, policy RotationPolicy) *Service {
	return &Service{
		store:   store,
		transit: transit,
		policy:  policy,
		kty:     "RSA",
	}
}

// EnsureSchema is a thin pass-through to the store.
func (s *Service) EnsureSchema(ctx context.Context) error {
	if err := s.store.EnsureSchema(ctx); err != nil {
		return jwksStoreErr(err)
	}
	return nil
}

// PublishedJwks returns the active + grace keys formatted for the
// /.well-known/jwks.json document. Triggers EnsureSeeded so a
// freshly-deployed service can serve a JWKS without manual
// intervention.
func (s *Service) PublishedJwks(ctx context.Context, now time.Time) (Jwks, error) {
	if err := s.ensureSeeded(ctx, now); err != nil {
		return Jwks{}, err
	}
	active, err := s.activeKey(ctx)
	if err != nil {
		return Jwks{}, err
	}
	grace, err := s.store.GraceKeys(ctx, now)
	if err != nil {
		return Jwks{}, jwksStoreErr(err)
	}
	return BuildJwksFromRecords(active, grace), nil
}

// Rotate creates a new Vault key version, demotes the current
// active key to grace, and returns the rotation outcome. The
// previous key remains in the JWKS until graceUntil.
func (s *Service) Rotate(ctx context.Context, now time.Time) (RotationOutcome, error) {
	if err := s.ensureSeeded(ctx, now); err != nil {
		return RotationOutcome{}, err
	}
	previous, err := s.activeKey(ctx)
	if err != nil {
		return RotationOutcome{}, err
	}
	nextRef, err := s.transit.RotateKey(ctx)
	if err != nil {
		return RotationOutcome{}, jwksVaultErr(err)
	}
	publicPEM, err := s.transit.PublicKeyPEM(ctx, nextRef)
	if err != nil {
		return RotationOutcome{}, jwksVaultErr(err)
	}
	next := s.recordFromVaultKey(nextRef, publicPEM, now)
	graceUntil := now.Add(time.Duration(s.policy.GraceDays) * 24 * time.Hour)
	if err := s.store.RotateTo(ctx, previous, next, graceUntil); err != nil {
		return RotationOutcome{}, jwksStoreErr(err)
	}
	return RotationOutcome{
		PreviousActiveKid: previous.Kid,
		ActiveKid:         next.Kid,
		GraceUntil:        graceUntil,
	}, nil
}

// Rollback restores a previously-graced key to active and pushes
// the current active back to grace. `targetKid == nil` picks the
// most recently-graced key.
func (s *Service) Rollback(ctx context.Context, targetKid *string, now time.Time) (RollbackOutcome, error) {
	if err := s.ensureSeeded(ctx, now); err != nil {
		return RollbackOutcome{}, err
	}
	active, err := s.activeKey(ctx)
	if err != nil {
		return RollbackOutcome{}, err
	}
	grace, err := s.store.GraceKeys(ctx, now)
	if err != nil {
		return RollbackOutcome{}, jwksStoreErr(err)
	}
	var restored JwksKeyRecord
	if targetKid != nil {
		found := false
		for _, k := range grace {
			if k.Kid == *targetKid {
				restored = k
				found = true
				break
			}
		}
		if !found {
			return RollbackOutcome{}, jwksStateErr("rollback target %s is not in grace", *targetKid)
		}
	} else {
		if len(grace) == 0 {
			return RollbackOutcome{}, jwksStateErr("no grace key to roll back to")
		}
		restored = grace[0] // grace_keys returns activated_at DESC
	}
	graceUntil := now.Add(time.Duration(s.policy.GraceDays) * 24 * time.Hour)
	if err := s.store.RollbackTo(ctx, restored, active, graceUntil); err != nil {
		return RollbackOutcome{}, jwksStoreErr(err)
	}
	return RollbackOutcome{
		RestoredActiveKid: restored.Kid,
		DemotedKid:        active.Kid,
		GraceUntil:        graceUntil,
	}, nil
}

// SignActive signs `digest` with the current active key. The
// returned SignedDigest carries the kid + key + raw signature
// bytes. JWS encoding (base64url etc.) is the caller's job.
func (s *Service) SignActive(ctx context.Context, digest []byte) (SignedDigest, error) {
	if err := s.ensureSeeded(ctx, time.Now().UTC()); err != nil {
		return SignedDigest{}, err
	}
	active, err := s.activeKey(ctx)
	if err != nil {
		return SignedDigest{}, err
	}
	key := active.VaultKeyRef()
	sig, err := s.transit.SignWithKey(ctx, key, digest)
	if err != nil {
		return SignedDigest{}, jwksVaultErr(err)
	}
	return SignedDigest{Kid: active.Kid, Key: key, Signature: sig}, nil
}

// ActiveSigningKey returns the kid + Vault reference for the
// currently-active key. Useful when the caller wants to populate a
// JWS header without performing a sign.
func (s *Service) ActiveSigningKey(ctx context.Context) (string, VaultKeyRef, error) {
	if err := s.ensureSeeded(ctx, time.Now().UTC()); err != nil {
		return "", VaultKeyRef{}, err
	}
	active, err := s.activeKey(ctx)
	if err != nil {
		return "", VaultKeyRef{}, err
	}
	return active.Kid, active.VaultKeyRef(), nil
}

// SignKey is a thin pass-through to the transit client for callers
// that already know which Vault key version to sign with (e.g. the
// in-flight JWS being verified by an old kid that's still in grace).
func (s *Service) SignKey(ctx context.Context, key VaultKeyRef, digest []byte) ([]byte, error) {
	sig, err := s.transit.SignWithKey(ctx, key, digest)
	if err != nil {
		return nil, jwksVaultErr(err)
	}
	return sig, nil
}

// ensureSeeded inserts a boot-time active row when the store is
// empty. No-op when an active row already exists.
func (s *Service) ensureSeeded(ctx context.Context, now time.Time) error {
	current, err := s.store.ActiveKey(ctx)
	if err != nil {
		return jwksStoreErr(err)
	}
	if current != nil {
		return nil
	}
	key, err := s.transit.LatestKeyRef(ctx)
	if err != nil {
		return jwksVaultErr(err)
	}
	publicPEM, err := s.transit.PublicKeyPEM(ctx, key)
	if err != nil {
		return jwksVaultErr(err)
	}
	if err := s.store.UpsertActiveSeed(ctx, s.recordFromVaultKey(key, publicPEM, now)); err != nil {
		return jwksStoreErr(err)
	}
	return nil
}

// activeKey fetches the active row, surfacing State("no active
// JWKS key") when the store is empty post-seed (which would
// indicate corruption).
func (s *Service) activeKey(ctx context.Context) (JwksKeyRecord, error) {
	r, err := s.store.ActiveKey(ctx)
	if err != nil {
		return JwksKeyRecord{}, jwksStoreErr(err)
	}
	if r == nil {
		return JwksKeyRecord{}, jwksStateErr("no active JWKS key")
	}
	return *r, nil
}

func (s *Service) recordFromVaultKey(key VaultKeyRef, publicPEM string, activatedAt time.Time) JwksKeyRecord {
	return JwksKeyRecord{
		Kid:             StableKid(key),
		Kty:             s.kty,
		PublicPEM:       publicPEM,
		VaultKeyName:    key.Name,
		VaultKeyVersion: key.Version,
		Status:          StatusActive,
		ActivatedAt:     activatedAt,
	}
}

// ─── Pure builders ───────────────────────────────────────────────────

// StableKid mirrors fn stable_kid: `<name>-v<version>`.
func StableKid(key VaultKeyRef) string {
	return fmt.Sprintf("%s-v%d", key.Name, key.Version)
}

// BuildJwks composes a JWKS from a (publication-only) active +
// optional previous key. Used by callers that hold PublicKeyEntry
// projections rather than full JwksKeyRecords. Mirrors fn
// build_jwks.
func BuildJwks(active PublicKeyEntry, previous *PublicKeyEntry, policy RotationPolicy, now time.Time) Jwks {
	keys := []JwkEntry{{
		Kid:       active.Kid,
		Kty:       active.Kty,
		PublicPEM: active.PublicPEM,
		Use:       "sig",
		Status:    "active",
	}}
	if previous != nil && policy.IsInGrace(previous.ActivatedAt, now) {
		keys = append(keys, JwkEntry{
			Kid:       previous.Kid,
			Kty:       previous.Kty,
			PublicPEM: previous.PublicPEM,
			Use:       "sig",
			Status:    "grace",
		})
	}
	return Jwks{Keys: keys}
}

// BuildJwksFromRecords composes a JWKS from a single active record
// + zero-or-more grace records. Mirrors fn build_jwks_from_records.
func BuildJwksFromRecords(active JwksKeyRecord, grace []JwksKeyRecord) Jwks {
	keys := []JwkEntry{jwkFromRecord(active, "active")}
	for i := range grace {
		keys = append(keys, jwkFromRecord(grace[i], "grace"))
	}
	return Jwks{Keys: keys}
}

func jwkFromRecord(record JwksKeyRecord, status string) JwkEntry {
	return JwkEntry{
		Kid:       record.Kid,
		Kty:       record.Kty,
		PublicPEM: record.PublicPEM,
		Use:       "sig",
		Status:    status,
	}
}

// ─── Schema constants ────────────────────────────────────────────────

// JwksKeysDDL is the table-creation statement used by Postgres
// JwksKeyStore implementations. Mirrors the Rust JWKS_KEYS_DDL
// constant verbatim.
const JwksKeysDDL = `CREATE TABLE IF NOT EXISTS jwks_keys (
  kid TEXT PRIMARY KEY,
  kty TEXT NOT NULL DEFAULT 'RSA',
  public_pem TEXT NOT NULL,
  vault_key_name TEXT NOT NULL,
  vault_key_version INTEGER NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'grace', 'retired')),
  activated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  grace_started_at TIMESTAMPTZ,
  retire_after TIMESTAMPTZ,
  retired_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// JwksKeysActiveIndexDDL keeps the active-key lookup O(log n).
const JwksKeysActiveIndexDDL = `CREATE INDEX IF NOT EXISTS jwks_keys_active_idx
  ON jwks_keys (activated_at DESC) WHERE status = 'active'`

// JwksKeysVersionIndexDDL serves the rare (vault_key_name, version)
// lookup used by the operator console.
const JwksKeysVersionIndexDDL = `CREATE INDEX IF NOT EXISTS jwks_keys_version_idx
  ON jwks_keys (vault_key_name, vault_key_version)`

// ─── Internal helper ────────────────────────────────────────────────

// guard against accidental copy-of-mutex compilation when sub-files
// embed the service.
var _ sync.Locker = (*sync.Mutex)(nil)
