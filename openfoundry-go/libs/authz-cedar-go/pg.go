package cedarauthz

import (
	"context"
	"fmt"
	"log/slog"

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
	pool  PgQuerier
	table string
	store *PolicyStore
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
	sql := fmt.Sprintf(
		`SELECT id, version, source, description
		   FROM (
		     SELECT DISTINCT ON (id) id, version, source, description
		       FROM %s
		      WHERE active = TRUE
		      ORDER BY id, version DESC
		   ) AS active_policies
		  ORDER BY id`, table)
	rows, err := p.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("%w: query %s: %v", ErrBackend, table, err)
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
