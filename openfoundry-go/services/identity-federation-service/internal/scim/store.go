package scim

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// UserRecord is the store-shape of a SCIM-managed user, mirroring
// the columns the Rust impl pulls off the `users` table. Combines
// the canonical User fields with the SCIM-specific
// `scim_external_id` column.
//
// Attributes is the JSONB blob that holds SCIM-specific extras —
// the conversion helpers (UserToScim, etc.) walk it for the
// /scim/openfoundry, /scim/externalId and /scim/name pointers.
type UserRecord struct {
	ID             uuid.UUID
	Email          string
	Name           string
	IsActive       bool
	OrganizationID *uuid.UUID
	Attributes     json.RawMessage
	ScimExternalID *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UserStore is the persistence-shaped contract the SCIM User
// surface delegates reads/writes to. Mirrors the SQL helpers in
// the Rust handler (load_user / list_users / count / etc.).
type UserStore interface {
	// Get returns the user with the given id, or (nil, nil) when
	// no row matches.
	Get(ctx context.Context, id uuid.UUID) (*UserRecord, error)
	// GetByExternalID is the SCIM-idempotency lookup. Returns
	// (nil, nil) when no row matches.
	GetByExternalID(ctx context.Context, externalID string) (*UserRecord, error)
	// List returns up to `count` users from `startIndex` (1-based,
	// SCIM convention) ordered by created_at DESC, optionally
	// filtered by an EqFilter. Returns (rows, total) where
	// total is the unpaginated count under the same filter.
	List(ctx context.Context, filter *EqFilter, startIndex, count int) ([]UserRecord, int, error)
	// Put inserts when no row with `record.ID` exists, or
	// updates in place when one does. The implementation MUST
	// honour the userName uniqueness constraint and surface a
	// distinguishable error so callers can map to 409 +
	// scimType="uniqueness".
	Put(ctx context.Context, record UserRecord) error
	// SoftDelete flips is_active = false (mirrors the Rust
	// "DELETE = deactivate" contract). Returns (false, nil) when
	// the user does not exist.
	SoftDelete(ctx context.Context, id uuid.UUID) (bool, error)
}

// IsUniqueViolation reports whether `err` carries the SCIM
// userName-uniqueness signal. The InMemoryUserStore returns
// ErrUserNameTaken; the Postgres impl (lands in 3.7b.3.4) will
// translate pgx unique-constraint errors. The handler maps both
// to 409 + scimType="uniqueness".
func IsUniqueViolation(err error) bool {
	return errors.Is(err, ErrUserNameTaken)
}

// ErrUserNameTaken is the canonical sentinel for a userName
// uniqueness conflict. Surfaced by InMemoryUserStore.Put when
// another row already owns the same email; mirrors the Rust
// is_unique_violation classifier.
var ErrUserNameTaken = errors.New("scim: userName already exists")

// ─── In-memory store ────────────────────────────────────────────────

// InMemoryUserStore is a thread-safe map-backed UserStore. Suitable
// for tests + local dev.
type InMemoryUserStore struct {
	mu   sync.RWMutex
	rows map[uuid.UUID]UserRecord
}

// NewInMemoryUserStore returns a freshly-initialised store.
func NewInMemoryUserStore() *InMemoryUserStore {
	return &InMemoryUserStore{rows: map[uuid.UUID]UserRecord{}}
}

// Compile-time interface assertion.
var _ UserStore = (*InMemoryUserStore)(nil)

// Insert inserts a row directly. Test helper, not part of the
// UserStore contract.
func (s *InMemoryUserStore) Insert(rec UserRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = rec.CreatedAt
	}
	s.rows[rec.ID] = rec
}

// Get satisfies UserStore.
func (s *InMemoryUserStore) Get(_ context.Context, id uuid.UUID) (*UserRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.rows[id]; ok {
		copy := r
		return &copy, nil
	}
	return nil, nil
}

// GetByExternalID satisfies UserStore.
func (s *InMemoryUserStore) GetByExternalID(_ context.Context, externalID string) (*UserRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rows {
		if r.ScimExternalID != nil && *r.ScimExternalID == externalID {
			copy := r
			return &copy, nil
		}
	}
	return nil, nil
}

// List satisfies UserStore.
func (s *InMemoryUserStore) List(_ context.Context, filter *EqFilter, startIndex, count int) ([]UserRecord, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]UserRecord, 0, len(s.rows))
	for _, r := range s.rows {
		if !matchesUserFilter(r, filter) {
			continue
		}
		matched = append(matched, r)
	}
	// Sort created_at DESC, breaking ties on id ASC for
	// determinism.
	sort.SliceStable(matched, func(i, j int) bool {
		if !matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].CreatedAt.After(matched[j].CreatedAt)
		}
		return strings.Compare(matched[i].ID.String(), matched[j].ID.String()) < 0
	})

	total := len(matched)
	// SCIM is 1-based.
	offset := startIndex - 1
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []UserRecord{}, total, nil
	}
	end := offset + count
	if end > total {
		end = total
	}
	out := make([]UserRecord, end-offset)
	copy(out, matched[offset:end])
	return out, total, nil
}

// matchesUserFilter is the in-memory predicate that mirrors the
// SQL WHERE clauses. Currently only userName / externalId apply
// to the User surface — displayName is for groups.
func matchesUserFilter(r UserRecord, filter *EqFilter) bool {
	if filter == nil {
		return true
	}
	switch filter.Attribute {
	case FilterUserName:
		return r.Email == filter.Value
	case FilterExternalID:
		return r.ScimExternalID != nil && *r.ScimExternalID == filter.Value
	}
	return false
}

// Put satisfies UserStore. Honours the userName uniqueness
// constraint by scanning for any other row with the same email and
// returning ErrUserNameTaken when found. Sets CreatedAt on
// insert, bumps UpdatedAt on every write.
func (s *InMemoryUserStore) Put(_ context.Context, record UserRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.rows {
		if id != record.ID && existing.Email == record.Email {
			return ErrUserNameTaken
		}
	}
	now := time.Now().UTC()
	if existing, ok := s.rows[record.ID]; ok {
		// Preserve the original CreatedAt on update.
		record.CreatedAt = existing.CreatedAt
	} else if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	s.rows[record.ID] = record
	return nil
}

// SoftDelete satisfies UserStore. Returns (false, nil) when the
// user doesn't exist; otherwise flips is_active = false +
// updated_at = NOW().
func (s *InMemoryUserStore) SoftDelete(_ context.Context, id uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.rows[id]
	if !ok {
		return false, nil
	}
	rec.IsActive = false
	rec.UpdatedAt = time.Now().UTC()
	s.rows[id] = rec
	return true, nil
}

// ErrUnsupportedFilter is returned when the caller asks the store
// to evaluate a filter the surface doesn't honour. Callers should
// surface this as a 400 with scimType=invalidFilter (the parser
// already does this; this sentinel is for adapter layers that
// don't go through ParseEqFilter).
var ErrUnsupportedFilter = errors.New("scim: unsupported filter attribute for this surface")
