package storage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gocql/gocql"

	cassandrakernel "github.com/openfoundry/openfoundry-go/libs/cassandra-kernel"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// NewCassandraStores adapts the production Cassandra kernel stores to the
// object-database-service storage interfaces.
func NewCassandraStores(session *gocql.Session, objectKeyspace, linkKeyspace string) (ObjectStore, LinkStore) {
	return NewCassandraObjectStore(cassandrakernel.NewObjectStoreWithKeyspaces(session, objectKeyspace, linkKeyspace)),
		NewCassandraLinkStore(cassandrakernel.NewLinkStoreWithKeyspace(session, linkKeyspace))
}

// CassandraObjectStore adapts libs/cassandra-kernel's repository-facing object
// store to the service-local ObjectStore interface used by HTTP handlers.
type CassandraObjectStore struct {
	inner repos.ObjectStore
}

func NewCassandraObjectStore(inner repos.ObjectStore) *CassandraObjectStore {
	return &CassandraObjectStore{inner: inner}
}

var _ ObjectStore = (*CassandraObjectStore)(nil)
var _ PointReadStore = (*CassandraObjectStore)(nil)
var _ PropertyQueryStore = (*CassandraObjectStore)(nil)

type repoPointReadStore interface {
	GetByTypeAndPrimaryKey(ctx context.Context, tenant repos.TenantId, typeID repos.TypeId, primaryKey string, consistency repos.ReadConsistency) (*repos.Object, error)
}

func (s *CassandraObjectStore) GetByTypeAndPrimaryKey(ctx context.Context, tenant TenantId, typeID TypeId, primaryKey string, c ReadConsistency) (*Object, error) {
	point, ok := s.inner.(repoPointReadStore)
	if !ok {
		obj, err := s.Get(ctx, tenant, ObjectId(primaryKey), c)
		if err != nil || obj == nil || obj.TypeID != typeID {
			return nil, err
		}
		return obj, nil
	}
	obj, err := point.GetByTypeAndPrimaryKey(ctx, repos.TenantId(tenant), repos.TypeId(typeID), primaryKey, toRepoConsistency(c))
	if err != nil {
		return nil, fromRepoError(err)
	}
	if obj == nil {
		return nil, nil
	}
	converted := fromRepoObject(*obj)
	return &converted, nil
}

type repoPropertyQueryStore interface {
	QueryByProperty(ctx context.Context, tenant repos.TenantId, typeID repos.TypeId, predicate repos.PropertyPredicate, page repos.Page, consistency repos.ReadConsistency) (repos.PagedResult[repos.Object], error)
}

func (s *CassandraObjectStore) QueryByProperty(ctx context.Context, tenant TenantId, typeID TypeId, predicate PropertyPredicate, page Page, c ReadConsistency) (PagedResult[Object], error) {
	indexed, ok := s.inner.(repoPropertyQueryStore)
	if !ok {
		return s.ListByType(ctx, tenant, typeID, page, c)
	}
	res, err := indexed.QueryByProperty(ctx, repos.TenantId(tenant), repos.TypeId(typeID), repos.PropertyPredicate{PropertyName: predicate.PropertyName, Operator: predicate.Operator, Value: predicate.Value}, toRepoPage(page), toRepoConsistency(c))
	if err != nil {
		return PagedResult[Object]{}, fromRepoError(err)
	}
	return fromRepoObjectPage(res), nil
}

func (s *CassandraObjectStore) Get(ctx context.Context, tenant TenantId, id ObjectId, c ReadConsistency) (*Object, error) {
	obj, err := s.inner.Get(ctx, repos.TenantId(tenant), repos.ObjectId(id), toRepoConsistency(c))
	if err != nil {
		return nil, fromRepoError(err)
	}
	if obj == nil {
		return nil, nil
	}
	converted := fromRepoObject(*obj)
	return &converted, nil
}

func (s *CassandraObjectStore) Put(ctx context.Context, obj Object, expectedVersion *uint64) (PutOutcome, error) {
	outcome, err := s.inner.Put(ctx, toRepoObject(obj), expectedVersion)
	if err != nil {
		return PutOutcome{}, fromRepoError(err)
	}
	return fromRepoPutOutcome(outcome), nil
}

func (s *CassandraObjectStore) Delete(ctx context.Context, tenant TenantId, id ObjectId) (bool, error) {
	deleted, err := s.inner.Delete(ctx, repos.TenantId(tenant), repos.ObjectId(id))
	return deleted, fromRepoError(err)
}

func (s *CassandraObjectStore) ListByType(ctx context.Context, tenant TenantId, typeID TypeId, page Page, c ReadConsistency) (PagedResult[Object], error) {
	res, err := s.inner.ListByType(ctx, repos.TenantId(tenant), repos.TypeId(typeID), toRepoPage(page), toRepoConsistency(c))
	if err != nil {
		return PagedResult[Object]{}, fromRepoError(err)
	}
	return fromRepoObjectPage(res), nil
}

func (s *CassandraObjectStore) ListByOwner(ctx context.Context, tenant TenantId, owner OwnerId, page Page, c ReadConsistency) (PagedResult[Object], error) {
	res, err := s.inner.ListByOwner(ctx, repos.TenantId(tenant), repos.OwnerId(owner), toRepoPage(page), toRepoConsistency(c))
	if err != nil {
		return PagedResult[Object]{}, fromRepoError(err)
	}
	return fromRepoObjectPage(res), nil
}

func (s *CassandraObjectStore) ListByMarking(ctx context.Context, tenant TenantId, marking MarkingId, page Page, c ReadConsistency) (PagedResult[Object], error) {
	res, err := s.inner.ListByMarking(ctx, repos.TenantId(tenant), repos.MarkingId(marking), toRepoPage(page), toRepoConsistency(c))
	if err != nil {
		return PagedResult[Object]{}, fromRepoError(err)
	}
	return fromRepoObjectPage(res), nil
}

// CassandraLinkStore adapts libs/cassandra-kernel's repository-facing link
// store to the service-local LinkStore interface used by HTTP handlers.
type CassandraLinkStore struct {
	inner repos.LinkStore
}

func NewCassandraLinkStore(inner repos.LinkStore) *CassandraLinkStore {
	return &CassandraLinkStore{inner: inner}
}

var _ LinkStore = (*CassandraLinkStore)(nil)

func (s *CassandraLinkStore) Put(ctx context.Context, link Link) error {
	return fromRepoError(s.inner.Put(ctx, toRepoLink(link)))
}

func (s *CassandraLinkStore) Delete(ctx context.Context, tenant TenantId, lt LinkTypeId, from, to ObjectId) (bool, error) {
	deleted, err := s.inner.Delete(ctx, repos.TenantId(tenant), repos.LinkTypeId(lt), repos.ObjectId(from), repos.ObjectId(to))
	return deleted, fromRepoError(err)
}

// DeleteIncident is intentionally a no-op on the Cassandra adapter.
// The production link tables (`ontology_links.{outgoing,incoming}`)
// are partitioned on (link_type_rid, src|dst), so a cross-link-type
// scan to find every incident edge of one object would degenerate
// into a full-table scan. The cassandra-kernel indexer / outbox is
// responsible for emitting per-(link_type, from, to) tombstones in
// response to the object delete; the handler degrades to "best
// effort" cascade in that case and the FK-equivalent cleanup is
// asynchronous. See ADR-0020 §S1.7.
//
// Tests that exercise cascade behaviour run against
// InMemoryLinkStore, which does the real cleanup synchronously.
func (s *CassandraLinkStore) DeleteIncident(_ context.Context, _ TenantId, _ ObjectId) (int, error) {
	return 0, nil
}

func (s *CassandraLinkStore) ListOutgoing(ctx context.Context, tenant TenantId, lt LinkTypeId, from ObjectId, page Page, c ReadConsistency) (PagedResult[Link], error) {
	res, err := s.inner.ListOutgoing(ctx, repos.TenantId(tenant), repos.LinkTypeId(lt), repos.ObjectId(from), toRepoPage(page), toRepoConsistency(c))
	if err != nil {
		return PagedResult[Link]{}, fromRepoError(err)
	}
	return fromRepoLinkPage(res), nil
}

func (s *CassandraLinkStore) ListIncoming(ctx context.Context, tenant TenantId, lt LinkTypeId, to ObjectId, page Page, c ReadConsistency) (PagedResult[Link], error) {
	res, err := s.inner.ListIncoming(ctx, repos.TenantId(tenant), repos.LinkTypeId(lt), repos.ObjectId(to), toRepoPage(page), toRepoConsistency(c))
	if err != nil {
		return PagedResult[Link]{}, fromRepoError(err)
	}
	return fromRepoLinkPage(res), nil
}

func toRepoConsistency(c ReadConsistency) repos.ReadConsistency {
	if c == ReadEventual {
		return repos.Eventual()
	}
	return repos.Strong()
}

func toRepoPage(page Page) repos.Page {
	return repos.Page{Size: page.Size, Token: page.Token}
}

func fromRepoObjectPage(res repos.PagedResult[repos.Object]) PagedResult[Object] {
	items := make([]Object, 0, len(res.Items))
	for _, item := range res.Items {
		items = append(items, fromRepoObject(item))
	}
	return PagedResult[Object]{Items: items, NextToken: res.NextToken}
}

func fromRepoLinkPage(res repos.PagedResult[repos.Link]) PagedResult[Link] {
	items := make([]Link, 0, len(res.Items))
	for _, item := range res.Items {
		items = append(items, fromRepoLink(item))
	}
	return PagedResult[Link]{Items: items, NextToken: res.NextToken}
}

func toRepoObject(obj Object) repos.Object {
	var owner *repos.OwnerId
	if obj.Owner != nil {
		v := repos.OwnerId(*obj.Owner)
		owner = &v
	}
	markings := make([]repos.MarkingId, 0, len(obj.Markings))
	for _, marking := range obj.Markings {
		markings = append(markings, repos.MarkingId(marking))
	}
	return repos.Object{
		Tenant:         repos.TenantId(obj.Tenant),
		ID:             repos.ObjectId(obj.ID),
		TypeID:         repos.TypeId(obj.TypeID),
		Version:        obj.Version,
		Payload:        obj.Payload,
		OrganizationID: obj.OrganizationID,
		CreatedAtMs:    obj.CreatedAtMs,
		UpdatedAtMs:    obj.UpdatedAtMs,
		Owner:          owner,
		Markings:       markings,
	}
}

func fromRepoObject(obj repos.Object) Object {
	var owner *OwnerId
	if obj.Owner != nil {
		v := OwnerId(*obj.Owner)
		owner = &v
	}
	markings := make([]MarkingId, 0, len(obj.Markings))
	for _, marking := range obj.Markings {
		markings = append(markings, MarkingId(marking))
	}
	return Object{
		Tenant:         TenantId(obj.Tenant),
		ID:             ObjectId(obj.ID),
		TypeID:         TypeId(obj.TypeID),
		Version:        obj.Version,
		Payload:        obj.Payload,
		OrganizationID: obj.OrganizationID,
		CreatedAtMs:    obj.CreatedAtMs,
		UpdatedAtMs:    obj.UpdatedAtMs,
		Owner:          owner,
		Markings:       markings,
	}
}

func toRepoLink(link Link) repos.Link {
	var payload json.RawMessage
	if link.Payload != nil {
		payload = *link.Payload
	}
	return repos.Link{
		Tenant:      repos.TenantId(link.Tenant),
		LinkType:    repos.LinkTypeId(link.LinkType),
		From:        repos.ObjectId(link.From),
		To:          repos.ObjectId(link.To),
		Payload:     payload,
		CreatedAtMs: link.CreatedAtMs,
	}
}

func fromRepoLink(link repos.Link) Link {
	var payload *json.RawMessage
	if len(link.Payload) > 0 {
		v := append(json.RawMessage(nil), link.Payload...)
		payload = &v
	}
	return Link{
		Tenant:      TenantId(link.Tenant),
		LinkType:    LinkTypeId(link.LinkType),
		From:        ObjectId(link.From),
		To:          ObjectId(link.To),
		Payload:     payload,
		CreatedAtMs: link.CreatedAtMs,
	}
}

func fromRepoPutOutcome(outcome repos.PutOutcome) PutOutcome {
	switch outcome.Kind {
	case repos.PutUpdated:
		return PutOutcome{Kind: PutUpdated, PreviousVersion: outcome.PreviousVersion, NewVersion: outcome.NewVersion}
	case repos.PutVersionConflict:
		return PutOutcome{Kind: PutVersionConflict, ExpectedVersion: outcome.ExpectedVersion, ActualVersion: outcome.ActualVersion}
	default:
		return PutOutcome{Kind: PutInserted}
	}
}

func fromRepoError(err error) error {
	if err == nil {
		return nil
	}
	var re *repos.RepoError
	if !errors.As(err, &re) {
		return err
	}
	kind := ErrBackend
	switch re.Kind {
	case repos.RepoNotFound:
		kind = ErrNotFound
	case repos.RepoInvalidArgument:
		kind = ErrInvalidArgument
	case repos.RepoTenantScope:
		kind = ErrTenantScope
	case repos.RepoBackend:
		kind = ErrBackend
	}
	return &RepoError{Kind: kind, Msg: re.Message}
}
