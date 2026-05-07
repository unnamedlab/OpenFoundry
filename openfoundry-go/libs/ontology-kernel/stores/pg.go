package stores

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// notYet is the verbatim message from
// `libs/ontology-kernel/src/stores/pg.rs::NOT_YET`. The Rust
// adapters return RepoError::Backend(NOT_YET); the Go adapters
// surface the same wrapped error so handlers see byte-identical
// strings during the migration window.
const notYet = "PostgreSQL adapter for storage-abstraction trait is a stub; the owning service has not yet been migrated. See migration-plan-cassandra-foundry-parity.md §S1.4-S1.7"

// PostgresObjectStore wraps a pgx pool and exposes [ObjectStore].
//
// **Status: stub.** Mirrors `pg.rs::PostgresObjectStore`. Every
// method returns RepoError(Backend, notYet) until the owning
// service migrates onto the trait per §S1.4–S1.7 of the
// Cassandra-Foundry parity plan.
type PostgresObjectStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresObjectStore mirrors `PostgresObjectStore::new(pool)`.
func NewPostgresObjectStore(pool *pgxpool.Pool) *PostgresObjectStore {
	return &PostgresObjectStore{Pool: pool}
}

// Compile-time conformance pin.
var _ storageabstraction.ObjectStore = (*PostgresObjectStore)(nil)

func (s *PostgresObjectStore) Get(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.ObjectId, _ storageabstraction.ReadConsistency) (*storageabstraction.Object, error) {
	return nil, storageabstraction.Backend(notYet)
}

func (s *PostgresObjectStore) Put(_ context.Context, _ storageabstraction.Object, _ *uint64) (storageabstraction.PutOutcome, error) {
	return storageabstraction.PutOutcome{}, storageabstraction.Backend(notYet)
}

func (s *PostgresObjectStore) Delete(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.ObjectId) (bool, error) {
	return false, storageabstraction.Backend(notYet)
}

func (s *PostgresObjectStore) ListByType(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.TypeId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Object], error) {
	return storageabstraction.PagedResult[storageabstraction.Object]{}, storageabstraction.Backend(notYet)
}

func (s *PostgresObjectStore) ListByOwner(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.OwnerId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Object], error) {
	return storageabstraction.PagedResult[storageabstraction.Object]{}, storageabstraction.Backend(notYet)
}

func (s *PostgresObjectStore) ListByMarking(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.MarkingId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Object], error) {
	return storageabstraction.PagedResult[storageabstraction.Object]{}, storageabstraction.Backend(notYet)
}

// PostgresLinkStore wraps a pgx pool and exposes [LinkStore].
//
// **Status: stub.** Mirrors `pg.rs::PostgresLinkStore`.
type PostgresLinkStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresLinkStore mirrors `PostgresLinkStore::new(pool)`.
func NewPostgresLinkStore(pool *pgxpool.Pool) *PostgresLinkStore {
	return &PostgresLinkStore{Pool: pool}
}

var _ storageabstraction.LinkStore = (*PostgresLinkStore)(nil)

func (s *PostgresLinkStore) Put(_ context.Context, _ storageabstraction.Link) error {
	return storageabstraction.Backend(notYet)
}

func (s *PostgresLinkStore) Delete(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.LinkTypeId, _ storageabstraction.ObjectId, _ storageabstraction.ObjectId) (bool, error) {
	return false, storageabstraction.Backend(notYet)
}

func (s *PostgresLinkStore) ListOutgoing(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.LinkTypeId, _ storageabstraction.ObjectId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Link], error) {
	return storageabstraction.PagedResult[storageabstraction.Link]{}, storageabstraction.Backend(notYet)
}

func (s *PostgresLinkStore) ListIncoming(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.LinkTypeId, _ storageabstraction.ObjectId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Link], error) {
	return storageabstraction.PagedResult[storageabstraction.Link]{}, storageabstraction.Backend(notYet)
}

// PostgresActionLogStore wraps a pgx pool and exposes [ActionLogStore].
//
// **Status: stub.** Mirrors `pg.rs::PostgresActionLogStore`.
type PostgresActionLogStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresActionLogStore mirrors `PostgresActionLogStore::new(pool)`.
func NewPostgresActionLogStore(pool *pgxpool.Pool) *PostgresActionLogStore {
	return &PostgresActionLogStore{Pool: pool}
}

var _ storageabstraction.ActionLogStore = (*PostgresActionLogStore)(nil)

func (s *PostgresActionLogStore) Append(_ context.Context, _ storageabstraction.ActionLogEntry) error {
	return storageabstraction.Backend(notYet)
}

func (s *PostgresActionLogStore) ListRecent(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.ActionLogEntry], error) {
	return storageabstraction.PagedResult[storageabstraction.ActionLogEntry]{}, storageabstraction.Backend(notYet)
}

func (s *PostgresActionLogStore) ListForObject(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.ObjectId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.ActionLogEntry], error) {
	return storageabstraction.PagedResult[storageabstraction.ActionLogEntry]{}, storageabstraction.Backend(notYet)
}

func (s *PostgresActionLogStore) ListForAction(_ context.Context, _ storageabstraction.TenantId, _ string, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.ActionLogEntry], error) {
	return storageabstraction.PagedResult[storageabstraction.ActionLogEntry]{}, storageabstraction.Backend(notYet)
}
