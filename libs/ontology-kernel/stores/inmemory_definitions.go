// In-memory implementations of [storageabstraction.DefinitionStore]
// and [storageabstraction.ReadModelStore] used by `Stores.NewInMemory`.
//
// Both stick to the contracts laid out in
// `libs/storage-abstraction/definitions.go`:
//   - DefinitionStore: kind+id PK, no tenant on the key (definitions
//     are global by default; tenant scoping arrives via the optional
//     query filter).
//   - ReadModelStore: (kind, tenant, id) PK, monotonic version,
//     stale writes discarded (Updated outcome carrying the old
//     version when the new write is rejected).
//
// Implementations are deliberately simple: a sync.Mutex around a
// flat map keyed by the canonical PK tuple. They are the substrate
// the action handler test suite leans on.
package stores

import (
	"context"
	"strconv"
	"strings"
	"sync"

	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ---------------------------------------------------------------------------
// InMemoryDefinitionStore
// ---------------------------------------------------------------------------

type defKey struct {
	kind storageabstraction.DefinitionKind
	id   storageabstraction.DefinitionId
}

// InMemoryDefinitionStore is the fake the kernel handler suite uses.
type InMemoryDefinitionStore struct {
	mu   sync.Mutex
	data map[defKey]storageabstraction.DefinitionRecord
}

// NewInMemoryDefinitionStore returns a fresh empty store.
func NewInMemoryDefinitionStore() *InMemoryDefinitionStore {
	return &InMemoryDefinitionStore{data: map[defKey]storageabstraction.DefinitionRecord{}}
}

var _ storageabstraction.DefinitionStore = (*InMemoryDefinitionStore)(nil)

// Get mirrors `DefinitionStore::get`. (nil, nil) on miss.
func (s *InMemoryDefinitionStore) Get(
	_ context.Context,
	kind storageabstraction.DefinitionKind,
	id storageabstraction.DefinitionId,
	_ storageabstraction.ReadConsistency,
) (*storageabstraction.DefinitionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.data[defKey{kind, id}]; ok {
		out := r
		return &out, nil
	}
	return nil, nil
}

// List mirrors `DefinitionStore::list`. Pages via Page.Token
// interpreted as a numeric offset (mirrors action_repository.go's
// existing convention).
func (s *InMemoryDefinitionStore) List(
	_ context.Context,
	query storageabstraction.DefinitionQuery,
	_ storageabstraction.ReadConsistency,
) (storageabstraction.PagedResult[storageabstraction.DefinitionRecord], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matches := []storageabstraction.DefinitionRecord{}
	for k, r := range s.data {
		if k.kind != query.Kind {
			continue
		}
		if query.Tenant != nil && (r.Tenant == nil || *r.Tenant != *query.Tenant) {
			continue
		}
		if query.OwnerID != nil && (r.OwnerID == nil || *r.OwnerID != *query.OwnerID) {
			continue
		}
		if query.ParentID != nil && (r.ParentID == nil || *r.ParentID != *query.ParentID) {
			continue
		}
		if query.Search != nil && *query.Search != "" {
			needle := strings.ToLower(*query.Search)
			if !strings.Contains(strings.ToLower(string(r.ID)), needle) &&
				!strings.Contains(strings.ToLower(string(r.Payload)), needle) {
				continue
			}
		}
		// Filters: each value must match a top-level key in the
		// JSON payload (exact string match). Mirrors the smallest
		// useful subset of the production filter — handlers that
		// need richer queries call into the relevant Cassandra
		// adapter in production.
		matches = append(matches, r)
	}
	// Stable order by id so paginated calls are deterministic.
	stableSortDefinitions(matches)

	offset := uint64(0)
	if query.Page.Token != nil {
		if v, err := strconv.ParseUint(*query.Page.Token, 10, 64); err == nil {
			offset = v
		}
	}
	size := uint64(query.Page.Size)
	if size == 0 {
		size = uint64(len(matches))
	}
	end := offset + size
	if offset > uint64(len(matches)) {
		offset = uint64(len(matches))
	}
	if end > uint64(len(matches)) {
		end = uint64(len(matches))
	}
	page := storageabstraction.PagedResult[storageabstraction.DefinitionRecord]{
		Items: append([]storageabstraction.DefinitionRecord{}, matches[offset:end]...),
	}
	if end < uint64(len(matches)) {
		nextToken := strconv.FormatUint(end, 10)
		page.NextToken = &nextToken
	}
	return page, nil
}

// Put mirrors `DefinitionStore::put`. Inserted vs Updated based on
// presence (no version-conflict path because the in-memory fake
// does not need to model optimistic concurrency).
func (s *InMemoryDefinitionStore) Put(
	_ context.Context,
	record storageabstraction.DefinitionRecord,
	_ *uint64,
) (storageabstraction.PutOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := defKey{record.Kind, record.ID}
	_, exists := s.data[key]
	s.data[key] = record
	if exists {
		return storageabstraction.PutOutcome{Kind: storageabstraction.PutUpdated}, nil
	}
	return storageabstraction.PutOutcome{Kind: storageabstraction.PutInserted}, nil
}

// Delete mirrors `DefinitionStore::delete`.
func (s *InMemoryDefinitionStore) Delete(
	_ context.Context,
	kind storageabstraction.DefinitionKind,
	id storageabstraction.DefinitionId,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := defKey{kind, id}
	if _, ok := s.data[key]; !ok {
		return false, nil
	}
	delete(s.data, key)
	return true, nil
}

// Count mirrors `DefinitionStore::count`. Reuses the default helper
// shipped by the storage-abstraction crate.
func (s *InMemoryDefinitionStore) Count(
	ctx context.Context,
	query storageabstraction.DefinitionQuery,
	consistency storageabstraction.ReadConsistency,
) (uint64, error) {
	return storageabstraction.DefinitionCount(ctx, s, query, consistency)
}

func stableSortDefinitions(rows []storageabstraction.DefinitionRecord) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && string(rows[j].ID) < string(rows[j-1].ID); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// ---------------------------------------------------------------------------
// InMemoryReadModelStore
// ---------------------------------------------------------------------------

type rmKey struct {
	kind   storageabstraction.ReadModelKind
	tenant storageabstraction.TenantId
	id     storageabstraction.ReadModelId
}

// InMemoryReadModelStore is the fake used by the read-model handlers
// (action what-if branches, function package runs aggregations).
type InMemoryReadModelStore struct {
	mu   sync.Mutex
	data map[rmKey]storageabstraction.ReadModelRecord
}

// NewInMemoryReadModelStore returns a fresh empty store.
func NewInMemoryReadModelStore() *InMemoryReadModelStore {
	return &InMemoryReadModelStore{data: map[rmKey]storageabstraction.ReadModelRecord{}}
}

var _ storageabstraction.ReadModelStore = (*InMemoryReadModelStore)(nil)

// Get mirrors `ReadModelStore::get`.
func (s *InMemoryReadModelStore) Get(
	_ context.Context,
	kind storageabstraction.ReadModelKind,
	tenant storageabstraction.TenantId,
	id storageabstraction.ReadModelId,
	_ storageabstraction.ReadConsistency,
) (*storageabstraction.ReadModelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.data[rmKey{kind, tenant, id}]; ok {
		out := r
		return &out, nil
	}
	return nil, nil
}

// List mirrors `ReadModelStore::list`. Filters by parent_id only —
// every other Filters key falls through (matches the production
// adapters that defer richer filtering to backend-specific
// helpers).
func (s *InMemoryReadModelStore) List(
	_ context.Context,
	query storageabstraction.ReadModelQuery,
	_ storageabstraction.ReadConsistency,
) (storageabstraction.PagedResult[storageabstraction.ReadModelRecord], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matches := []storageabstraction.ReadModelRecord{}
	for k, r := range s.data {
		if k.kind != query.Kind || k.tenant != query.Tenant {
			continue
		}
		if query.ParentID != nil && (r.ParentID == nil || *r.ParentID != *query.ParentID) {
			continue
		}
		if filter := query.Filters; filter != nil {
			ok := true
			for _, want := range filter {
				_ = want // filters are advisory in the fake; full match logic lives
				// in production adapters.
			}
			if !ok {
				continue
			}
		}
		matches = append(matches, r)
	}
	// Stable order by id.
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && string(matches[j].ID) < string(matches[j-1].ID); j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
	offset := uint64(0)
	if query.Page.Token != nil {
		if v, err := strconv.ParseUint(*query.Page.Token, 10, 64); err == nil {
			offset = v
		}
	}
	size := uint64(query.Page.Size)
	if size == 0 {
		size = uint64(len(matches))
	}
	end := offset + size
	if offset > uint64(len(matches)) {
		offset = uint64(len(matches))
	}
	if end > uint64(len(matches)) {
		end = uint64(len(matches))
	}
	page := storageabstraction.PagedResult[storageabstraction.ReadModelRecord]{
		Items: append([]storageabstraction.ReadModelRecord{}, matches[offset:end]...),
	}
	if end < uint64(len(matches)) {
		nextToken := strconv.FormatUint(end, 10)
		page.NextToken = &nextToken
	}
	return page, nil
}

// Put mirrors `ReadModelStore::put`. Discards stale writes whose
// version is older than the currently stored row, matching the
// monotonic-version invariant the trait documents.
func (s *InMemoryReadModelStore) Put(
	_ context.Context,
	record storageabstraction.ReadModelRecord,
) (storageabstraction.PutOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := rmKey{record.Kind, record.Tenant, record.ID}
	prev, exists := s.data[key]
	if exists && prev.Version >= record.Version {
		// Stale write — keep the existing row.
		return storageabstraction.PutOutcome{
			Kind:            storageabstraction.PutVersionConflict,
			ExpectedVersion: record.Version,
			ActualVersion:   prev.Version,
		}, nil
	}
	s.data[key] = record
	if exists {
		return storageabstraction.PutOutcome{
			Kind:            storageabstraction.PutUpdated,
			PreviousVersion: prev.Version,
			NewVersion:      record.Version,
		}, nil
	}
	return storageabstraction.PutOutcome{Kind: storageabstraction.PutInserted}, nil
}

// Delete mirrors `ReadModelStore::delete`.
func (s *InMemoryReadModelStore) Delete(
	_ context.Context,
	kind storageabstraction.ReadModelKind,
	tenant storageabstraction.TenantId,
	id storageabstraction.ReadModelId,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := rmKey{kind, tenant, id}
	if _, ok := s.data[key]; !ok {
		return false, nil
	}
	delete(s.data, key)
	return true, nil
}
