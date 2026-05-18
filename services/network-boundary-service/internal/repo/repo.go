// Package repo embeds the SQL migrations and exposes a Postgres-backed
// implementation of handler.EgressPolicyStore. Every policy is stored
// as a single row whose deep nested fields (approval tasks, audit
// events, workload usages, decorations) live in a JSONB blob. The
// handler logic remains the source of truth for mutations; the repo
// reads under a row-level lock, lets the handler rebuild the policy,
// then writes the new JSONB back atomically.
package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/handler"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies the embedded SQL migrations in lexical order. Idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// PgEgressPolicyStore persists handler.NetworkEgressPolicy rows in Postgres.
type PgEgressPolicyStore struct {
	pool *pgxpool.Pool
	// memDelegate hosts the pure decoration / state-transition logic.
	// We use it to keep the inventory-dependent invariants (overlap
	// detection, risk warnings) byte-for-byte identical to the
	// in-memory path: load the row(s), hand them to memDelegate, write
	// the result back.
	memDelegate *handler.MemoryEgressPolicyStore
}

// NewPgEgressPolicyStore returns a Postgres-backed store. The pool must
// already be connected.
func NewPgEgressPolicyStore(pool *pgxpool.Pool) *PgEgressPolicyStore {
	return &PgEgressPolicyStore{pool: pool, memDelegate: handler.NewMemoryEgressPolicyStore()}
}

// ListPolicies returns every persisted policy, freshly decorated with
// inventory-dependent fields (overlap IDs, risk warnings).
func (s *PgEgressPolicyStore) ListPolicies(ctx context.Context) ([]handler.NetworkEgressPolicy, error) {
	policies, err := s.loadAll(ctx)
	if err != nil {
		return nil, err
	}
	return s.decorateAll(ctx, policies)
}

// CreatePolicy inserts a freshly built policy row. The handler has
// already validated the payload and produced the full policy struct.
func (s *PgEgressPolicyStore) CreatePolicy(ctx context.Context, policy handler.NetworkEgressPolicy) (handler.NetworkEgressPolicy, error) {
	inventory, err := s.loadAll(ctx)
	if err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	inventory = append(inventory, policy)
	decorated, err := s.decorateAll(ctx, inventory)
	if err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	target := findPolicy(decorated, policy.ID)
	if target == nil {
		return handler.NetworkEgressPolicy{}, fmt.Errorf("decorate inserted policy: missing %s", policy.ID)
	}
	if err := s.insertRow(ctx, *target); err != nil {
		return handler.NetworkEgressPolicy{}, fmt.Errorf("insert policy: %w", err)
	}
	return *target, nil
}

// GetPolicy returns the policy by id, decorated against the live inventory.
func (s *PgEgressPolicyStore) GetPolicy(ctx context.Context, id string) (handler.NetworkEgressPolicy, bool, error) {
	if strings.TrimSpace(id) == "" {
		return handler.NetworkEgressPolicy{}, false, nil
	}
	inventory, err := s.loadAll(ctx)
	if err != nil {
		return handler.NetworkEgressPolicy{}, false, err
	}
	decorated, err := s.decorateAll(ctx, inventory)
	if err != nil {
		return handler.NetworkEgressPolicy{}, false, err
	}
	target := findPolicy(decorated, id)
	if target == nil {
		return handler.NetworkEgressPolicy{}, false, nil
	}
	return *target, true, nil
}

// ListApprovals returns approval tasks filtered by status across every
// stored policy.
func (s *PgEgressPolicyStore) ListApprovals(ctx context.Context, status string) ([]handler.EgressApprovalTask, error) {
	policies, err := s.loadAll(ctx)
	if err != nil {
		return nil, err
	}
	out := []handler.EgressApprovalTask{}
	status = strings.TrimSpace(status)
	for _, policy := range policies {
		for _, task := range policy.ApprovalTasks {
			if status != "" && task.Status != status {
				continue
			}
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RequestedAt.After(out[j].RequestedAt)
	})
	return out, nil
}

// RequestStateChange records a pending approval task targeting `state`.
func (s *PgEgressPolicyStore) RequestStateChange(ctx context.Context, id, state, actor, reason string) (handler.NetworkEgressPolicy, error) {
	return s.runWithDelegate(ctx, []string{id}, func(d *handler.MemoryEgressPolicyStore) (handler.NetworkEgressPolicy, error) {
		return d.RequestStateChange(ctx, id, state, actor, reason)
	})
}

// DecideApproval resolves the approval task and, if approved, applies
// the requested transition.
func (s *PgEgressPolicyStore) DecideApproval(ctx context.Context, taskID, decision, actor, reason string) (handler.NetworkEgressPolicy, handler.EgressApprovalTask, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return handler.NetworkEgressPolicy{}, handler.EgressApprovalTask{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	policies, err := s.loadAllTx(ctx, tx, true)
	if err != nil {
		return handler.NetworkEgressPolicy{}, handler.EgressApprovalTask{}, err
	}
	delegate := handler.NewMemoryEgressPolicyStore()
	if err := delegate.Seed(ctx, policies); err != nil {
		return handler.NetworkEgressPolicy{}, handler.EgressApprovalTask{}, fmt.Errorf("seed delegate: %w", err)
	}
	policy, task, err := delegate.DecideApproval(ctx, taskID, decision, actor, reason)
	if err != nil {
		return handler.NetworkEgressPolicy{}, handler.EgressApprovalTask{}, err
	}
	if err := s.upsertRowTx(ctx, tx, policy); err != nil {
		return handler.NetworkEgressPolicy{}, handler.EgressApprovalTask{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return handler.NetworkEgressPolicy{}, handler.EgressApprovalTask{}, err
	}
	return policy, task, nil
}

// UpdateState approves and applies a lifecycle transition in one step.
func (s *PgEgressPolicyStore) UpdateState(ctx context.Context, id, state, actor, reason string) (handler.NetworkEgressPolicy, error) {
	return s.runWithDelegate(ctx, []string{id}, func(d *handler.MemoryEgressPolicyStore) (handler.NetworkEgressPolicy, error) {
		return d.UpdateState(ctx, id, state, actor, reason)
	})
}

// UpdateSharing replaces viewer / importer / admin grants and records
// the audit trail.
func (s *PgEgressPolicyStore) UpdateSharing(ctx context.Context, id string, viewer, importer, admin []string, actor, reason string) (handler.NetworkEgressPolicy, error) {
	return s.runWithDelegate(ctx, []string{id}, func(d *handler.MemoryEgressPolicyStore) (handler.NetworkEgressPolicy, error) {
		return d.UpdateSharing(ctx, id, viewer, importer, admin, actor, reason)
	})
}

// RecordRuntimeUse persists workload-usage telemetry for a runtime decision.
func (s *PgEgressPolicyStore) RecordRuntimeUse(ctx context.Context, policyID string, claims *authmw.Claims, body handler.EvaluateWorkloadEgressRequest, decision handler.EgressPolicyRuntimeDecision) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, ok, err := s.loadRowTx(ctx, tx, policyID, true)
	if err != nil {
		return err
	}
	if !ok {
		return handler.ErrEgressPolicyNotFound
	}
	delegate := handler.NewMemoryEgressPolicyStore()
	if err := delegate.Seed(ctx, []handler.NetworkEgressPolicy{row}); err != nil {
		return fmt.Errorf("seed delegate: %w", err)
	}
	if err := delegate.RecordRuntimeUse(ctx, policyID, claims, body, decision); err != nil {
		return err
	}
	updated, ok, err := delegate.GetPolicy(ctx, policyID)
	if err != nil {
		return err
	}
	if !ok {
		return handler.ErrEgressPolicyNotFound
	}
	if err := s.upsertRowTx(ctx, tx, updated); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// runWithDelegate performs `read locked rows → mutate via in-memory
// store → write back → commit` as one transaction so concurrent
// mutations don't trample each other.
func (s *PgEgressPolicyStore) runWithDelegate(ctx context.Context, _ []string, op func(*handler.MemoryEgressPolicyStore) (handler.NetworkEgressPolicy, error)) (handler.NetworkEgressPolicy, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	policies, err := s.loadAllTx(ctx, tx, true)
	if err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	delegate := handler.NewMemoryEgressPolicyStore()
	if err := delegate.Seed(ctx, policies); err != nil {
		return handler.NetworkEgressPolicy{}, fmt.Errorf("seed delegate: %w", err)
	}
	updated, err := op(delegate)
	if err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	if err := s.upsertRowTx(ctx, tx, updated); err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return handler.NetworkEgressPolicy{}, err
	}
	return updated, nil
}

func (s *PgEgressPolicyStore) loadAll(ctx context.Context) ([]handler.NetworkEgressPolicy, error) {
	rows, err := s.pool.Query(ctx, `SELECT policy FROM network_egress_policies`)
	if err != nil {
		return nil, fmt.Errorf("query policies: %w", err)
	}
	defer rows.Close()
	return scanPolicies(rows)
}

func (s *PgEgressPolicyStore) loadAllTx(ctx context.Context, tx pgx.Tx, lock bool) ([]handler.NetworkEgressPolicy, error) {
	q := `SELECT policy FROM network_egress_policies`
	if lock {
		q += ` FOR UPDATE`
	}
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query policies: %w", err)
	}
	defer rows.Close()
	return scanPolicies(rows)
}

func (s *PgEgressPolicyStore) loadRowTx(ctx context.Context, tx pgx.Tx, id string, lock bool) (handler.NetworkEgressPolicy, bool, error) {
	q := `SELECT policy FROM network_egress_policies WHERE id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	row := tx.QueryRow(ctx, q, id)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return handler.NetworkEgressPolicy{}, false, nil
		}
		return handler.NetworkEgressPolicy{}, false, err
	}
	var policy handler.NetworkEgressPolicy
	if err := json.Unmarshal(blob, &policy); err != nil {
		return handler.NetworkEgressPolicy{}, false, fmt.Errorf("decode policy %s: %w", id, err)
	}
	return policy, true, nil
}

func (s *PgEgressPolicyStore) insertRow(ctx context.Context, policy handler.NetworkEgressPolicy) error {
	blob, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("encode policy: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO network_egress_policies
		    (id, name, kind, state, created_by, created_at, updated_at, version, policy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8::jsonb)`,
		policy.ID, policy.Name, policy.Kind, policy.State, policy.CreatedBy,
		policy.CreatedAt, policy.UpdatedAt, blob,
	)
	return err
}

// upsertRowTx writes the policy row, inserting on absence so writes
// can land in a transaction that started from a SELECT FOR UPDATE
// snapshot (the row may have been created within the same transaction
// by an earlier op).
func (s *PgEgressPolicyStore) upsertRowTx(ctx context.Context, tx pgx.Tx, policy handler.NetworkEgressPolicy) error {
	blob, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("encode policy: %w", err)
	}
	cmd, err := tx.Exec(ctx, `
		INSERT INTO network_egress_policies
		    (id, name, kind, state, created_by, created_at, updated_at, version, policy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8::jsonb)
		ON CONFLICT (id) DO UPDATE SET
		    name       = EXCLUDED.name,
		    kind       = EXCLUDED.kind,
		    state      = EXCLUDED.state,
		    updated_at = EXCLUDED.updated_at,
		    version    = network_egress_policies.version + 1,
		    policy     = EXCLUDED.policy`,
		policy.ID, policy.Name, policy.Kind, policy.State, policy.CreatedBy,
		policy.CreatedAt, policy.UpdatedAt, blob,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return handler.ErrEgressPolicyNotFound
	}
	return nil
}

// decorateAll re-runs the inventory-dependent decoration step using
// the in-memory store as a pure function (Seed → Snapshot). This keeps
// overlap detection and risk warnings byte-for-byte identical to the
// in-process path.
func (s *PgEgressPolicyStore) decorateAll(ctx context.Context, policies []handler.NetworkEgressPolicy) ([]handler.NetworkEgressPolicy, error) {
	delegate := handler.NewMemoryEgressPolicyStore()
	if err := delegate.Seed(ctx, policies); err != nil {
		return nil, fmt.Errorf("seed delegate: %w", err)
	}
	return delegate.ListPolicies(ctx)
}

func scanPolicies(rows pgx.Rows) ([]handler.NetworkEgressPolicy, error) {
	out := make([]handler.NetworkEgressPolicy, 0)
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		var policy handler.NetworkEgressPolicy
		if err := json.Unmarshal(blob, &policy); err != nil {
			return nil, fmt.Errorf("decode policy row: %w", err)
		}
		out = append(out, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func findPolicy(policies []handler.NetworkEgressPolicy, id string) *handler.NetworkEgressPolicy {
	for i := range policies {
		if policies[i].ID == id {
			return &policies[i]
		}
	}
	return nil
}
