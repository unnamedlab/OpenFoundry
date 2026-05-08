package jwksrotation

import (
	"context"
	"sort"
	"sync"
	"time"
)

// ─── InMemoryJwksKeyStore ───────────────────────────────────────────

// InMemoryJwksKeyStore is a thread-safe map-backed JwksKeyStore
// useful for tests and local-first dev. Mirrors
// noop::InMemoryJwksKeyStore semantics.
type InMemoryJwksKeyStore struct {
	mu   sync.Mutex
	rows map[string]JwksKeyRecord // keyed by kid
}

// NewInMemoryStore returns a freshly-initialised store.
func NewInMemoryStore() *InMemoryJwksKeyStore {
	return &InMemoryJwksKeyStore{rows: map[string]JwksKeyRecord{}}
}

// EnsureSchema is a no-op for the in-memory store.
func (s *InMemoryJwksKeyStore) EnsureSchema(_ context.Context) error { return nil }

// ActiveKey returns the single row with status=active, or nil when
// the store is empty.
func (s *InMemoryJwksKeyStore) ActiveKey(_ context.Context) (*JwksKeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *JwksKeyRecord
	for _, r := range s.rows {
		r := r
		if r.Status != StatusActive {
			continue
		}
		if best == nil || r.ActivatedAt.After(best.ActivatedAt) {
			best = &r
		}
	}
	return best, nil
}

// GraceKeys returns every "grace" row whose retire_after is either
// nil or strictly after `now`, sorted by ActivatedAt DESC.
func (s *InMemoryJwksKeyStore) GraceKeys(_ context.Context, now time.Time) ([]JwksKeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JwksKeyRecord, 0)
	for _, r := range s.rows {
		if r.Status != StatusGrace {
			continue
		}
		if r.RetireAfter != nil && !r.RetireAfter.After(now) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ActivatedAt.After(out[j].ActivatedAt) })
	return out, nil
}

// UpsertActiveSeed mirrors the Rust ON CONFLICT (kid) DO UPDATE
// shape. Does NOT touch the previous active row; callers that need
// rotation atomicity use RotateTo.
func (s *InMemoryJwksKeyStore) UpsertActiveSeed(_ context.Context, record JwksKeyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.rows[record.Kid]; ok {
		existing.PublicPEM = record.PublicPEM
		existing.Status = StatusActive
		existing.RetiredAt = nil
		s.rows[record.Kid] = existing
		return nil
	}
	rec := record
	rec.Status = StatusActive
	s.rows[record.Kid] = rec
	return nil
}

// RotateTo atomically demotes `previous` to grace + activates
// `next`. The in-memory impl serialises both writes under the
// store mutex, matching the Postgres tx.
func (s *InMemoryJwksKeyStore) RotateTo(_ context.Context, previous JwksKeyRecord, next JwksKeyRecord, graceUntil time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	prevRow := s.rows[previous.Kid]
	prevRow.Status = StatusGrace
	prevRow.GraceStartedAt = &now
	prevRow.RetireAfter = &graceUntil
	s.rows[previous.Kid] = prevRow

	if existing, ok := s.rows[next.Kid]; ok {
		existing.PublicPEM = next.PublicPEM
		existing.ActivatedAt = next.ActivatedAt
		existing.Status = StatusActive
		existing.GraceStartedAt = nil
		existing.RetireAfter = nil
		existing.RetiredAt = nil
		s.rows[next.Kid] = existing
	} else {
		nextRow := next
		nextRow.Status = StatusActive
		nextRow.GraceStartedAt = nil
		nextRow.RetireAfter = nil
		nextRow.RetiredAt = nil
		s.rows[next.Kid] = nextRow
	}
	return nil
}

// RollbackTo atomically demotes `demoted` to grace + restores
// `restored` to active.
func (s *InMemoryJwksKeyStore) RollbackTo(_ context.Context, restored JwksKeyRecord, demoted JwksKeyRecord, graceUntil time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	demRow := s.rows[demoted.Kid]
	demRow.Status = StatusGrace
	demRow.GraceStartedAt = &now
	demRow.RetireAfter = &graceUntil
	s.rows[demoted.Kid] = demRow

	resRow := s.rows[restored.Kid]
	resRow.Status = StatusActive
	resRow.GraceStartedAt = nil
	resRow.RetireAfter = nil
	resRow.RetiredAt = nil
	s.rows[restored.Kid] = resRow
	return nil
}

// AllRows returns a snapshot of every row, sorted by Kid. Test-only
// helper not exposed on the JwksKeyStore interface.
func (s *InMemoryJwksKeyStore) AllRows() []JwksKeyRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JwksKeyRecord, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kid < out[j].Kid })
	return out
}

// Compile-time interface assertion.
var _ JwksKeyStore = (*InMemoryJwksKeyStore)(nil)

// ─── FakeTransitKeyClient ───────────────────────────────────────────

// FakeTransitKeyClient is a deterministic, in-memory
// TransitKeyClient that mints PEMs by formatting "PEM(<name>-v<n>)"
// and produces signatures by HMACing the digest with a fixed key.
// Useful for testing the orchestrator without a live Vault.
//
// Keys are addressed by name + version. RotateKey bumps the version
// of the configured key by 1. SignWithKey returns deterministic
// bytes derived from name + version + digest so callers can
// round-trip.
type FakeTransitKeyClient struct {
	mu             sync.Mutex
	configured     VaultKeyRef
	highestVersion uint32
}

// NewFakeTransitKeyClient seeds the fake with a single key
// (name, version=1).
func NewFakeTransitKeyClient(name string) *FakeTransitKeyClient {
	return &FakeTransitKeyClient{
		configured:     VaultKeyRef{Name: name, Version: 1},
		highestVersion: 1,
	}
}

// ConfiguredKeyRef returns the boot-time reference. The version on
// this struct stays at 1 (the configured boot version) even after
// RotateKey bumps the latest version — mirrors Rust semantics.
func (c *FakeTransitKeyClient) ConfiguredKeyRef() VaultKeyRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.configured
}

// LatestKeyRef returns the most-recently-rotated key.
func (c *FakeTransitKeyClient) LatestKeyRef(_ context.Context) (VaultKeyRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return VaultKeyRef{Name: c.configured.Name, Version: c.highestVersion}, nil
}

// RotateKey bumps the version + returns the new reference.
func (c *FakeTransitKeyClient) RotateKey(_ context.Context) (VaultKeyRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.highestVersion++
	return VaultKeyRef{Name: c.configured.Name, Version: c.highestVersion}, nil
}

// PublicKeyPEM returns a deterministic stand-in PEM that round-trips
// through the JWKS pipeline. The shape — `PEM(<name>-v<n>)` —
// makes test failures readable.
func (c *FakeTransitKeyClient) PublicKeyPEM(_ context.Context, key VaultKeyRef) (string, error) {
	return "PEM(" + StableKid(key) + ")", nil
}

// SignWithKey returns a deterministic signature over the digest +
// key. The bytes are not cryptographically meaningful but are
// stable, so tests can assert exact equality.
func (c *FakeTransitKeyClient) SignWithKey(_ context.Context, key VaultKeyRef, digest []byte) ([]byte, error) {
	prefix := []byte(StableKid(key) + ":")
	out := make([]byte, 0, len(prefix)+len(digest))
	out = append(out, prefix...)
	out = append(out, digest...)
	return out, nil
}

// Compile-time interface assertion.
var _ TransitKeyClient = (*FakeTransitKeyClient)(nil)
