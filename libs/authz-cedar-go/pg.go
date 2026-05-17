package cedarauthz

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PgQuerier is the narrow surface of pgxpool.Pool we need for policy
// loading. Defined here so tests can drive the loader with a mock pool
// (e.g. pgxmock.PgxPoolIface) without depending on a real Postgres.
type PgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// DefaultPolicyTable is the default Postgres table name read by
// [PgPolicyStore.Reload]. Override with [PgPolicyStore.WithTable].
const DefaultPolicyTable = "cedar_policies"

// PgPolicyStore is a Postgres-backed loader for [*PolicyStore].
//
// Expected schema (managed by the bootstrap migration in
// services/authorization-policy-service):
//
//	CREATE TABLE IF NOT EXISTS cedar_policies (
//	    id          TEXT        PRIMARY KEY,
//	    version     INT         NOT NULL,
//	    source      TEXT        NOT NULL,
//	    description TEXT,
//	    active      BOOLEAN     NOT NULL DEFAULT TRUE,
//	    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
//
// Only the highest `version` per `id` is loaded, and only rows where
// `active = TRUE`. Matches the contract documented in ADR-0027.
type PgPolicyStore struct {
	pool     PgQuerier
	table    string
	store    *PolicyStore
	tenantID *uuid.UUID // nil = platform/global only; non-nil = tenant + globals.
}

// NewPgPolicyStore wires a loader against the bundled [*PolicyStore].
// `pool` is typically a *pgxpool.Pool from libs/db-pool; tests pass a
// pgxmock.PgxPoolIface (anything satisfying [PgQuerier] works).
//
// Cloning the returned struct is cheap; the inner store + pool are
// shared by every handle.
func NewPgPolicyStore(pool PgQuerier, store *PolicyStore) *PgPolicyStore {
	return &PgPolicyStore{pool: pool, table: DefaultPolicyTable, store: store}
}

// WithTable overrides the source table (defaults to `cedar_policies`).
// Returns the receiver so callers can chain.
//
// The table name is sanitised at read time (alphanumeric + underscore
// only) — never user input — so the SQL `format` is injection-safe.
func (p *PgPolicyStore) WithTable(table string) *PgPolicyStore {
	p.table = table
	return p
}

// WithTenantID scopes subsequent [Reload] calls to a single tenant
// plus any platform-global rows (`tenant_id IS NULL`). Passing nil
// reverts to the platform/admin view (globals only).
//
// Tenant scoping is the read-side counterpart to the
// authorization-policy-service Repo.ListCedarPolicies tenant filter:
// a downstream service hydrating its engine for a specific tenant
// must call this so cross-tenant policies never enter the in-memory
// PolicySet.
func (p *PgPolicyStore) WithTenantID(tenantID *uuid.UUID) *PgPolicyStore {
	p.tenantID = tenantID
	return p
}

// Store returns the underlying [*PolicyStore] handle.
func (p *PgPolicyStore) Store() *PolicyStore { return p.store }

// Reload reads every active policy from Postgres and atomically swaps
// them into the inner [*PolicyStore]. Idempotent — call from startup
// and from the NATS `authz.policy.changed` handler.
//
// Returns the number of policies loaded.
func (p *PgPolicyStore) Reload(ctx context.Context) (int, error) {
	records, err := p.fetchActive(ctx)
	if err != nil {
		return 0, err
	}
	if err := p.store.ReplacePolicies(records); err != nil {
		return 0, err
	}
	slog.Info("cedar policies reloaded",
		slog.Int("policies", len(records)),
		slog.String("table", p.table),
	)
	return len(records), nil
}

func (p *PgPolicyStore) fetchActive(ctx context.Context) ([]PolicyRecord, error) {
	table, err := p.sanitisedTable()
	if err != nil {
		return nil, err
	}
	// DISTINCT ON (id) ORDER BY id, version DESC keeps only the latest
	// version per id. Postgres guarantees the ordering applies before
	// the DISTINCT ON deduplication.
	//
	// Tenant scoping is applied inside the inner subquery so a stale
	// tenant row can't bubble up via the deduplication step.
	var (
		rows pgx.Rows
		qerr error
	)
	if p.tenantID == nil {
		sql := fmt.Sprintf(
			`SELECT id, version, source, description
			   FROM (
			     SELECT DISTINCT ON (id) id, version, source, description
			       FROM %s
			      WHERE active = TRUE
			        AND tenant_id IS NULL
			      ORDER BY id, version DESC
			   ) AS active_policies
			  ORDER BY id`, table)
		rows, qerr = p.pool.Query(ctx, sql)
	} else {
		sql := fmt.Sprintf(
			`SELECT id, version, source, description
			   FROM (
			     SELECT DISTINCT ON (id) id, version, source, description
			       FROM %s
			      WHERE active = TRUE
			        AND (tenant_id = $1 OR tenant_id IS NULL)
			      ORDER BY id, version DESC
			   ) AS active_policies
			  ORDER BY id`, table)
		rows, qerr = p.pool.Query(ctx, sql, *p.tenantID)
	}
	if qerr != nil {
		return nil, fmt.Errorf("%w: query %s: %v", ErrBackend, table, qerr)
	}
	defer rows.Close()

	out := make([]PolicyRecord, 0)
	for rows.Next() {
		var rec PolicyRecord
		if err := rows.Scan(&rec.ID, &rec.Version, &rec.Source, &rec.Description); err != nil {
			return nil, fmt.Errorf("%w: scan: %v", ErrBackend, err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: rows: %v", ErrBackend, err)
	}
	return out, nil
}

// ─── Tenant-scoped engine cache ─────────────────────────────────────

// TenantPolicyCache holds a per-tenant [*PolicyStore], hydrated lazily
// from the shared `cedar_policies` table. Each tenant gets its own
// in-memory PolicySet so evaluation never spans tenants — the only
// shared resources are the Postgres pool and the bundled schema.
//
// A monotonically increasing `version` token is stamped per tenant on
// every Reload; downstream callers (e.g. the NATS reload subscriber)
// can compare a cached token against the cache's current view to
// decide whether to invalidate. The token is opaque — only equality
// is meaningful.
type TenantPolicyCache struct {
	pool  PgQuerier
	table string

	mu      sync.RWMutex
	entries map[uuid.UUID]*tenantPolicyEntry
	global  *tenantPolicyEntry // tenant_id IS NULL view, keyed separately.
}

type tenantPolicyEntry struct {
	store   *PgPolicyStore
	version uint64
}

// NewTenantPolicyCache wires an empty cache against `pool`. Override
// the source table with [TenantPolicyCache.WithTable] before any
// [TenantPolicyCache.Reload] call.
func NewTenantPolicyCache(pool PgQuerier) *TenantPolicyCache {
	return &TenantPolicyCache{
		pool:    pool,
		table:   DefaultPolicyTable,
		entries: make(map[uuid.UUID]*tenantPolicyEntry),
	}
}

// WithTable overrides the source table (defaults to `cedar_policies`).
func (c *TenantPolicyCache) WithTable(table string) *TenantPolicyCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.table = table
	return c
}

// Get returns the (cached) PolicyStore for `tenantID`, hydrating it on
// first access. nil tenantID returns the platform/global view.
//
// The returned token must be paired with the store: passing it back to
// [TenantPolicyCache.Invalidate] is the supported way to drop a stale
// view without racing concurrent Reloads.
func (c *TenantPolicyCache) Get(ctx context.Context, tenantID *uuid.UUID) (*PolicyStore, uint64, error) {
	if entry := c.lookup(tenantID); entry != nil {
		return entry.store.Store(), entry.version, nil
	}
	return c.hydrate(ctx, tenantID)
}

// Reload forces a fresh fetch for `tenantID`. Returns the new policy
// count + the bumped version token.
func (c *TenantPolicyCache) Reload(ctx context.Context, tenantID *uuid.UUID) (int, uint64, error) {
	c.mu.Lock()
	entry := c.entryLocked(tenantID)
	if entry == nil {
		// Build the loader under the lock so the store pointer the
		// cache returns to readers is stable for the lifetime of the
		// entry — Reload only swaps the version + policy set inside.
		entry = c.buildEntryLocked(tenantID)
		c.storeEntryLocked(tenantID, entry)
	}
	store := entry.store
	c.mu.Unlock()

	count, err := store.Reload(ctx)
	if err != nil {
		return 0, 0, err
	}

	c.mu.Lock()
	entry.version++
	v := entry.version
	c.mu.Unlock()
	return count, v, nil
}

// Invalidate drops the cached entry for `tenantID` so the next [Get]
// rebuilds it from scratch.
func (c *TenantPolicyCache) Invalidate(tenantID *uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tenantID == nil {
		c.global = nil
		return
	}
	delete(c.entries, *tenantID)
}

// InvalidateAll drops every cached entry. Use sparingly — typically
// only on schema-level changes or process startup.
func (c *TenantPolicyCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[uuid.UUID]*tenantPolicyEntry)
	c.global = nil
}

func (c *TenantPolicyCache) lookup(tenantID *uuid.UUID) *tenantPolicyEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entryLocked(tenantID)
}

func (c *TenantPolicyCache) entryLocked(tenantID *uuid.UUID) *tenantPolicyEntry {
	if tenantID == nil {
		return c.global
	}
	return c.entries[*tenantID]
}

func (c *TenantPolicyCache) storeEntryLocked(tenantID *uuid.UUID, entry *tenantPolicyEntry) {
	if tenantID == nil {
		c.global = entry
		return
	}
	c.entries[*tenantID] = entry
}

func (c *TenantPolicyCache) buildEntryLocked(tenantID *uuid.UUID) *tenantPolicyEntry {
	store, err := NewEmpty()
	if err != nil {
		// NewEmpty only fails if the bundled schema is unparseable,
		// which is a process-startup invariant violation — surfacing
		// it as a panic is fine because the binary can't serve auth.
		panic(fmt.Errorf("cedarauthz: bundled schema unparseable: %w", err))
	}
	loader := NewPgPolicyStore(c.pool, store).WithTable(c.table).WithTenantID(tenantID)
	return &tenantPolicyEntry{store: loader}
}

func (c *TenantPolicyCache) hydrate(ctx context.Context, tenantID *uuid.UUID) (*PolicyStore, uint64, error) {
	c.mu.Lock()
	entry := c.entryLocked(tenantID)
	if entry == nil {
		entry = c.buildEntryLocked(tenantID)
		c.storeEntryLocked(tenantID, entry)
	}
	store := entry.store
	c.mu.Unlock()

	if _, err := store.Reload(ctx); err != nil {
		return nil, 0, err
	}

	c.mu.Lock()
	entry.version++
	v := entry.version
	c.mu.Unlock()
	return store.Store(), v, nil
}

// sanitisedTable rejects anything that isn't a bare ASCII identifier
// (alphanumeric + underscore) so the format above can never emit a
// SQL-injection vector.
func (p *PgPolicyStore) sanitisedTable() (string, error) {
	if p.table == "" {
		return "", fmt.Errorf("%w: empty table name", ErrBackend)
	}
	for _, r := range p.table {
		if !(r >= 'a' && r <= 'z') &&
			!(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') &&
			r != '_' {
			return "", fmt.Errorf("%w: invalid table name %q", ErrBackend, p.table)
		}
	}
	return p.table, nil
}
