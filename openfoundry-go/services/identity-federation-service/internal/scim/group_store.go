package scim

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// GroupRecord is the store-shape of a SCIM-managed group.
// Mirrors the columns the Rust impl pulls off the `groups` table:
// id + name + scim_external_id.
type GroupRecord struct {
	ID             uuid.UUID
	Name           string
	ScimExternalID *string
}

// MemberView is one resolved (user_id, email, name) tuple
// returned by GroupStore.Members. Mirrors the Rust SQL JOIN
// `groups → group_members → users` projection. The handler maps
// this to a ScimGroupMember (value=id, ref=/Users/{id},
// display=name|email, type="User") in GroupToScim.
type MemberView struct {
	UserID uuid.UUID
	Email  string
	Name   string
}

// GroupStore is the persistence-shaped contract the SCIM Group
// surface delegates reads/writes to. Mirrors the SQL helpers in
// the Rust handler (load_group / list_groups /
// load_group_members / insert_group_members_tx /
// replace_group_members_tx / remove_group_members_tx).
//
// Member-related calls accept a list of user UUIDs the caller has
// already parsed from `ScimGroupMember.value` strings. Stores must
// surface ErrMemberNotFound when any user id does not resolve so
// the handler can map to 400 invalidValue (mirrors the Rust FK
// violation → "group member does not reference an existing user").
type GroupStore interface {
	// Get returns the group with the given id, or (nil, nil) when
	// no row matches.
	Get(ctx context.Context, id uuid.UUID) (*GroupRecord, error)
	// GetByExternalID is the SCIM-idempotency lookup. Returns
	// (nil, nil) when no row matches.
	GetByExternalID(ctx context.Context, externalID string) (*GroupRecord, error)
	// List returns up to `count` groups from `startIndex` (1-based,
	// SCIM convention) ordered by name ASC, optionally filtered
	// by an EqFilter (DisplayName / ExternalID).
	List(ctx context.Context, filter *EqFilter, startIndex, count int) ([]GroupRecord, int, error)
	// Put inserts when no row with `record.ID` exists, or updates
	// in place when one does. Implementations MUST surface
	// ErrGroupNameTaken when another row already owns the same
	// name (the displayName uniqueness invariant).
	Put(ctx context.Context, record GroupRecord) error
	// Delete removes the row entirely. Returns (false, nil) when
	// no row matches.
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	// Members returns the (user_id, email, name) tuples
	// associated with `groupID`, ordered by email ASC. Mirrors
	// fn load_group_members.
	Members(ctx context.Context, groupID uuid.UUID) ([]MemberView, error)
	// AddMembers inserts (groupID, userID) rows. Idempotent
	// (ON CONFLICT DO NOTHING). Returns ErrMemberNotFound when
	// any userID does not resolve.
	AddMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error
	// ReplaceMembers atomically removes every (groupID, *)
	// membership and inserts the new set.
	ReplaceMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error
	// RemoveAllMembers atomically clears the membership set.
	RemoveAllMembers(ctx context.Context, groupID uuid.UUID) error
	// RemoveMember removes a single membership tuple. Idempotent.
	RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error
}

// ErrGroupNameTaken is the canonical sentinel for a displayName
// uniqueness conflict on Group.Put. Surfaced by InMemoryGroupStore
// when another row already owns the same name. Postgres impl
// translates the unique-constraint error to this sentinel.
var ErrGroupNameTaken = errors.New("scim: displayName already exists")

// IsGroupUniqueViolation classifies a Put error as a
// displayName-uniqueness conflict. Handler maps to 409 +
// scimType="uniqueness".
func IsGroupUniqueViolation(err error) bool {
	return errors.Is(err, ErrGroupNameTaken)
}

// ErrMemberNotFound is the canonical sentinel for an unknown user
// id in AddMembers/ReplaceMembers. Handler maps to 400 +
// scimType="invalidValue" with the wire detail "group member does
// not reference an existing user" (matches Rust verbatim).
var ErrMemberNotFound = errors.New("scim: group member does not reference an existing user")

// IsMemberNotFound classifies an AddMembers / ReplaceMembers
// error as a missing-user reference.
func IsMemberNotFound(err error) bool {
	return errors.Is(err, ErrMemberNotFound)
}

// ─── InMemoryGroupStore ────────────────────────────────────────────

// InMemoryGroupStore is a thread-safe map-backed GroupStore
// useful for tests + local dev. Resolves member views against an
// underlying UserStore so the wire shape stays faithful (display
// name + email).
type InMemoryGroupStore struct {
	mu      sync.RWMutex
	rows    map[uuid.UUID]GroupRecord
	members map[uuid.UUID]map[uuid.UUID]struct{}
	users   UserStore
}

// NewInMemoryGroupStore returns a freshly-initialised store
// backed by `users`. Member views are resolved live against the
// user store on every Members call.
func NewInMemoryGroupStore(users UserStore) *InMemoryGroupStore {
	return &InMemoryGroupStore{
		rows:    map[uuid.UUID]GroupRecord{},
		members: map[uuid.UUID]map[uuid.UUID]struct{}{},
		users:   users,
	}
}

// Insert is a test helper that primes a row directly without
// going through Put's uniqueness gate.
func (s *InMemoryGroupStore) Insert(rec GroupRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[rec.ID] = rec
}

// Get satisfies GroupStore.
func (s *InMemoryGroupStore) Get(_ context.Context, id uuid.UUID) (*GroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.rows[id]; ok {
		copy := r
		return &copy, nil
	}
	return nil, nil
}

// GetByExternalID satisfies GroupStore.
func (s *InMemoryGroupStore) GetByExternalID(_ context.Context, externalID string) (*GroupRecord, error) {
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

// List satisfies GroupStore. Orders by name ASC for stability;
// falls back to id ASC on duplicate names.
func (s *InMemoryGroupStore) List(_ context.Context, filter *EqFilter, startIndex, count int) ([]GroupRecord, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]GroupRecord, 0, len(s.rows))
	for _, r := range s.rows {
		if !matchesGroupFilter(r, filter) {
			continue
		}
		matched = append(matched, r)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Name != matched[j].Name {
			return matched[i].Name < matched[j].Name
		}
		return strings.Compare(matched[i].ID.String(), matched[j].ID.String()) < 0
	})

	total := len(matched)
	offset := startIndex - 1
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []GroupRecord{}, total, nil
	}
	end := offset + count
	if end > total {
		end = total
	}
	out := make([]GroupRecord, end-offset)
	copy(out, matched[offset:end])
	return out, total, nil
}

// Put satisfies GroupStore. Honours displayName uniqueness across
// other rows.
func (s *InMemoryGroupStore) Put(_ context.Context, record GroupRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.rows {
		if id != record.ID && existing.Name == record.Name {
			return ErrGroupNameTaken
		}
	}
	s.rows[record.ID] = record
	if _, ok := s.members[record.ID]; !ok {
		s.members[record.ID] = map[uuid.UUID]struct{}{}
	}
	return nil
}

// Delete satisfies GroupStore.
func (s *InMemoryGroupStore) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return false, nil
	}
	delete(s.rows, id)
	delete(s.members, id)
	return true, nil
}

// Members satisfies GroupStore. Returns each member resolved to
// (user_id, email, name) ordered by email ASC. Skips members
// whose user id is no longer present in the user store
// (mirrors the Rust INNER JOIN behaviour).
func (s *InMemoryGroupStore) Members(ctx context.Context, groupID uuid.UUID) ([]MemberView, error) {
	s.mu.RLock()
	memberSet, ok := s.members[groupID]
	if !ok {
		s.mu.RUnlock()
		return []MemberView{}, nil
	}
	ids := make([]uuid.UUID, 0, len(memberSet))
	for id := range memberSet {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	out := make([]MemberView, 0, len(ids))
	for _, id := range ids {
		user, err := s.users.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if user == nil {
			continue
		}
		out = append(out, MemberView{
			UserID: user.ID,
			Email:  user.Email,
			Name:   user.Name,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// AddMembers satisfies GroupStore. Validates each user exists
// before mutating; returns ErrMemberNotFound on first miss
// (none of the new ids are persisted). Idempotent on the
// already-present subset.
func (s *InMemoryGroupStore) AddMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error {
	for _, id := range userIDs {
		user, err := s.users.Get(ctx, id)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrMemberNotFound
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.members[groupID]; !ok {
		s.members[groupID] = map[uuid.UUID]struct{}{}
	}
	for _, id := range userIDs {
		s.members[groupID][id] = struct{}{}
	}
	return nil
}

// ReplaceMembers satisfies GroupStore. Validates first, then
// atomically clears + inserts under the store mutex.
func (s *InMemoryGroupStore) ReplaceMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error {
	for _, id := range userIDs {
		user, err := s.users.Get(ctx, id)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrMemberNotFound
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh := map[uuid.UUID]struct{}{}
	for _, id := range userIDs {
		fresh[id] = struct{}{}
	}
	s.members[groupID] = fresh
	return nil
}

// RemoveAllMembers satisfies GroupStore.
func (s *InMemoryGroupStore) RemoveAllMembers(_ context.Context, groupID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[groupID] = map[uuid.UUID]struct{}{}
	return nil
}

// RemoveMember satisfies GroupStore.
func (s *InMemoryGroupStore) RemoveMember(_ context.Context, groupID, userID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if set, ok := s.members[groupID]; ok {
		delete(set, userID)
	}
	return nil
}

// matchesGroupFilter is the in-memory predicate that mirrors the
// SQL WHERE clauses for the Group surface.
func matchesGroupFilter(r GroupRecord, filter *EqFilter) bool {
	if filter == nil {
		return true
	}
	switch filter.Attribute {
	case FilterDisplayName:
		return r.Name == filter.Value
	case FilterExternalID:
		return r.ScimExternalID != nil && *r.ScimExternalID == filter.Value
	}
	return false
}

// Compile-time interface assertion.
var _ GroupStore = (*InMemoryGroupStore)(nil)
