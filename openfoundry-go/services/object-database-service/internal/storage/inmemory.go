package storage

import (
	"context"
	"sort"
	"sync"
)

// InMemoryObjectStore is a concurrency-safe map fake. Drop-in for
// dev / unit tests. Mirror of `storage_abstraction::repositories::noop`.
type InMemoryObjectStore struct {
	mu   sync.Mutex
	rows map[objectKey]Object
}

type objectKey struct {
	tenant TenantId
	id     ObjectId
}

func NewInMemoryObjectStore() *InMemoryObjectStore {
	return &InMemoryObjectStore{rows: make(map[objectKey]Object)}
}

func (s *InMemoryObjectStore) Get(_ context.Context, tenant TenantId, id ObjectId, _ ReadConsistency) (*Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.rows[objectKey{tenant, id}]
	if !ok {
		return nil, nil
	}
	cp := obj
	return &cp, nil
}

func (s *InMemoryObjectStore) Put(_ context.Context, obj Object, expectedVersion *uint64) (PutOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := objectKey{obj.Tenant, obj.ID}
	existing, present := s.rows[key]

	switch {
	case !present && (expectedVersion == nil || *expectedVersion == 0):
		toInsert := obj
		toInsert.Version = 1
		s.rows[key] = toInsert
		return PutOutcome{Kind: PutInserted}, nil
	case !present:
		return PutOutcome{
			Kind:            PutVersionConflict,
			ExpectedVersion: *expectedVersion,
			ActualVersion:   0,
		}, nil
	default:
		if expectedVersion != nil && *expectedVersion != existing.Version {
			return PutOutcome{
				Kind:            PutVersionConflict,
				ExpectedVersion: *expectedVersion,
				ActualVersion:   existing.Version,
			}, nil
		}
		newVersion := existing.Version + 1
		toUpdate := obj
		toUpdate.Version = newVersion
		s.rows[key] = toUpdate
		return PutOutcome{
			Kind:            PutUpdated,
			PreviousVersion: existing.Version,
			NewVersion:      newVersion,
		}, nil
	}
}

func (s *InMemoryObjectStore) Delete(_ context.Context, tenant TenantId, id ObjectId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := objectKey{tenant, id}
	if _, ok := s.rows[key]; !ok {
		return false, nil
	}
	delete(s.rows, key)
	return true, nil
}

func (s *InMemoryObjectStore) snapshot() []Object {
	out := make([]Object, 0, len(s.rows))
	for _, v := range s.rows {
		out = append(out, v)
	}
	return out
}

func (s *InMemoryObjectStore) listFiltered(filter func(Object) bool, page Page) PagedResult[Object] {
	s.mu.Lock()
	rows := s.snapshot()
	s.mu.Unlock()

	items := make([]Object, 0, len(rows))
	for _, o := range rows {
		if filter(o) {
			items = append(items, o)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAtMs > items[j].UpdatedAtMs
	})
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return PagedResult[Object]{Items: items, NextToken: nil}
}

func (s *InMemoryObjectStore) ListByType(_ context.Context, tenant TenantId, typeID TypeId, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		return o.Tenant == tenant && o.TypeID == typeID
	}, page), nil
}

func (s *InMemoryObjectStore) ListByOwner(_ context.Context, tenant TenantId, owner OwnerId, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		return o.Tenant == tenant && o.Owner != nil && *o.Owner == owner
	}, page), nil
}

func (s *InMemoryObjectStore) ListByMarking(_ context.Context, tenant TenantId, marking MarkingId, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		if o.Tenant != tenant {
			return false
		}
		for _, m := range o.Markings {
			if m == marking {
				return true
			}
		}
		return false
	}, page), nil
}

// InMemoryLinkStore is the link-side counterpart.
type InMemoryLinkStore struct {
	mu   sync.Mutex
	rows []Link
}

func NewInMemoryLinkStore() *InMemoryLinkStore {
	return &InMemoryLinkStore{}
}

func (s *InMemoryLinkStore) Put(_ context.Context, link Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.rows {
		if l.Tenant == link.Tenant && l.LinkType == link.LinkType && l.From == link.From && l.To == link.To {
			return nil
		}
	}
	s.rows = append(s.rows, link)
	return nil
}

func (s *InMemoryLinkStore) Delete(_ context.Context, tenant TenantId, lt LinkTypeId, from, to ObjectId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.rows)
	kept := s.rows[:0]
	for _, l := range s.rows {
		if l.Tenant == tenant && l.LinkType == lt && l.From == from && l.To == to {
			continue
		}
		kept = append(kept, l)
	}
	s.rows = kept
	return len(s.rows) != before, nil
}

func (s *InMemoryLinkStore) listLinks(filter func(Link) bool, page Page) PagedResult[Link] {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	items := make([]Link, 0, limit)
	for _, l := range s.rows {
		if filter(l) {
			items = append(items, l)
			if len(items) >= limit {
				break
			}
		}
	}
	return PagedResult[Link]{Items: items, NextToken: nil}
}

func (s *InMemoryLinkStore) ListOutgoing(_ context.Context, tenant TenantId, lt LinkTypeId, from ObjectId, page Page, _ ReadConsistency) (PagedResult[Link], error) {
	return s.listLinks(func(l Link) bool {
		return l.Tenant == tenant && l.LinkType == lt && l.From == from
	}, page), nil
}

func (s *InMemoryLinkStore) ListIncoming(_ context.Context, tenant TenantId, lt LinkTypeId, to ObjectId, page Page, _ ReadConsistency) (PagedResult[Link], error) {
	return s.listLinks(func(l Link) bool {
		return l.Tenant == tenant && l.LinkType == lt && l.To == to
	}, page), nil
}
