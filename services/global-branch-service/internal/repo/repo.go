// Package repo holds pgx-backed persistence + embedded SQL migrations
// for global-branch-service.
//
// Two collaborating tables: global_branches and
// global_branch_participations. Lifecycle mutations that need to
// publish an audit event take a pgx.Tx so the SQL write and the
// outbox enqueue commit atomically (ADR-0022).
package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration in lexicographic order.
// Mirrors the naive runner used by the rest of the service fleet —
// each file is re-applied on every pod start and must therefore be
// idempotent.
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

// Repo is the pgx-backed implementation.
type Repo struct{ Pool *pgxpool.Pool }

// New builds a Repo using the supplied pool.
func New(pool *pgxpool.Pool) *Repo { return &Repo{Pool: pool} }

// BeginTx starts a transaction on the underlying pool. The caller is
// responsible for Commit / Rollback — every mutation that emits an
// audit event flows through this entry point.
func (r *Repo) BeginTx(ctx context.Context) (pgx.Tx, error) { return r.Pool.Begin(ctx) }

// ── global_branches ────────────────────────────────────────────────

const branchSelect = `SELECT id, tenant_id, name, base_ref, status, description,
		created_by, created_at, merged_at, merged_by FROM global_branches`

// CreateBranch inserts a fresh row. The caller is expected to have run
// domain.ValidateNew already; this method maps a UNIQUE collision on
// (tenant_id, name) to a typed conflict error so handlers can produce
// a 409 without parsing the pg error themselves.
func (r *Repo) CreateBranch(ctx context.Context, tx pgx.Tx, b *domain.GlobalBranch) (*domain.GlobalBranch, error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.Status == "" {
		b.Status = domain.StatusOpen
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	row := tx.QueryRow(ctx,
		`INSERT INTO global_branches
		    (id, tenant_id, name, base_ref, status, description, created_by, created_at)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		  RETURNING id, tenant_id, name, base_ref, status, description,
		            created_by, created_at, merged_at, merged_by`,
		b.ID, b.TenantID, b.Name, b.BaseRef, string(b.Status), b.Description, b.CreatedBy, b.CreatedAt,
	)
	out, err := scanBranch(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, &ErrBranchNameConflict{TenantID: b.TenantID, Name: b.Name}
		}
		return nil, err
	}
	return out, nil
}

// GetBranch fetches a single row. Returns (nil, domain.ErrBranchNotFound)
// when the id does not exist or belongs to a different tenant.
//
// The tenant filter is enforced at the SQL level so a forged ID from a
// caller in tenant A cannot read a branch from tenant B.
func (r *Repo) GetBranch(ctx context.Context, tenantID, id uuid.UUID) (*domain.GlobalBranch, error) {
	row := r.Pool.QueryRow(ctx, branchSelect+` WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	v, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBranchNotFound
	}
	return v, err
}

// GetBranchTx is the in-transaction variant used by the mutating
// endpoints (abandon, merge, register-participant). It keeps the read
// + write under the same MVCC snapshot so the lifecycle check is not
// racy.
func (r *Repo) GetBranchTx(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*domain.GlobalBranch, error) {
	row := tx.QueryRow(ctx, branchSelect+` WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, id, tenantID)
	v, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBranchNotFound
	}
	return v, err
}

// ListFilter narrows ListBranches.
type ListFilter struct {
	TenantID uuid.UUID
	Status   domain.BranchStatus // empty → all
	Limit    uint32
}

// ListBranches returns up to Limit branches for the tenant, optionally
// filtered by status. Limit==0 falls back to 100 and is capped at 500.
func (r *Repo) ListBranches(ctx context.Context, f ListFilter) ([]domain.GlobalBranch, error) {
	limit := f.Limit
	if limit == 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var (
		rows pgx.Rows
		err  error
	)
	if f.Status != "" {
		rows, err = r.Pool.Query(ctx,
			branchSelect+` WHERE tenant_id = $1 AND status = $2
			    ORDER BY created_at DESC LIMIT $3`,
			f.TenantID, string(f.Status), int32(limit))
	} else {
		rows, err = r.Pool.Query(ctx,
			branchSelect+` WHERE tenant_id = $1
			    ORDER BY created_at DESC LIMIT $2`,
			f.TenantID, int32(limit))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.GlobalBranch, 0)
	for rows.Next() {
		v, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// UpdateMetadataParams captures the fields PATCH can touch. Only
// non-nil fields are applied.
type UpdateMetadataParams struct {
	Name        *string
	Description *string
}

// UpdateMetadata applies a partial update. Returns the post-update
// row or domain.ErrBranchNotFound. Conflicts on (tenant_id, name)
// surface as ErrBranchNameConflict so handlers can produce a 409.
func (r *Repo) UpdateMetadata(ctx context.Context, tenantID, id uuid.UUID, p UpdateMetadataParams) (*domain.GlobalBranch, error) {
	current, err := r.GetBranch(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	name := current.Name
	if p.Name != nil {
		trimmed := strings.TrimSpace(*p.Name)
		if trimmed == "" {
			return nil, errors.New("global-branch: name cannot be empty")
		}
		name = trimmed
	}
	description := current.Description
	if p.Description != nil {
		description = strings.TrimSpace(*p.Description)
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE global_branches SET name = $3, description = $4
		   WHERE id = $1 AND tenant_id = $2
		 RETURNING id, tenant_id, name, base_ref, status, description,
		           created_by, created_at, merged_at, merged_by`,
		id, tenantID, name, description,
	)
	out, err := scanBranch(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, &ErrBranchNameConflict{TenantID: tenantID, Name: name}
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBranchNotFound
		}
		return nil, err
	}
	return out, nil
}

// SetStatus advances the branch status inside the supplied tx. The
// caller is responsible for the lifecycle rule check (domain layer);
// this method is the pure SQL write. Merged status additionally
// stamps merged_at + merged_by.
func (r *Repo) SetStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status domain.BranchStatus, mergedBy *uuid.UUID, mergedAt *time.Time) (*domain.GlobalBranch, error) {
	row := tx.QueryRow(ctx,
		`UPDATE global_branches SET
		    status     = $3,
		    merged_at  = CASE WHEN $3 = 'merged' THEN COALESCE($4, now()) ELSE merged_at END,
		    merged_by  = CASE WHEN $3 = 'merged' THEN $5                  ELSE merged_by END
		   WHERE id = $1 AND tenant_id = $2
		 RETURNING id, tenant_id, name, base_ref, status, description,
		           created_by, created_at, merged_at, merged_by`,
		id, tenantID, string(status), mergedAt, mergedBy,
	)
	out, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBranchNotFound
	}
	return out, err
}

// ── global_branch_participations ───────────────────────────────────

// AddParticipation inserts a participation row. Returns
// domain.ErrParticipationExists on UNIQUE collision so the handler can
// answer 409.
func (r *Repo) AddParticipation(ctx context.Context, tx pgx.Tx, p *domain.Participation) (*domain.Participation, error) {
	if p.Status == "" {
		p.Status = domain.ParticipationPending
	}
	row := tx.QueryRow(ctx,
		`INSERT INTO global_branch_participations
		    (global_branch_id, service_name, local_branch_ref, status, last_synced_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING global_branch_id, service_name, local_branch_ref, status, last_synced_at`,
		p.GlobalBranchID, p.ServiceName, p.LocalBranchRef, string(p.Status), p.LastSyncedAt,
	)
	out, err := scanParticipation(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, domain.ErrParticipationExists
		}
		return nil, err
	}
	return out, nil
}

// ListParticipations returns every participation row for a branch.
// Used to populate the participating_services slice on the branch
// response and as the merge precondition input.
func (r *Repo) ListParticipations(ctx context.Context, branchID uuid.UUID) ([]domain.Participation, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT global_branch_id, service_name, local_branch_ref, status, last_synced_at
		   FROM global_branch_participations
		  WHERE global_branch_id = $1
		  ORDER BY service_name ASC`,
		branchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Participation, 0)
	for rows.Next() {
		v, err := scanParticipation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// ListParticipationsTx is the in-transaction variant the merge path
// uses so the conflict precondition is consistent with the row lock
// taken on the branch.
func (r *Repo) ListParticipationsTx(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) ([]domain.Participation, error) {
	rows, err := tx.Query(ctx,
		`SELECT global_branch_id, service_name, local_branch_ref, status, last_synced_at
		   FROM global_branch_participations
		  WHERE global_branch_id = $1
		  ORDER BY service_name ASC`,
		branchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Participation, 0)
	for rows.Next() {
		v, err := scanParticipation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// RemoveParticipation deletes a single (branch, service) row. Returns
// whether anything was deleted so handlers can answer 404 cleanly.
func (r *Repo) RemoveParticipation(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, service string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`DELETE FROM global_branch_participations
		  WHERE global_branch_id = $1 AND service_name = $2`,
		branchID, service,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkAllParticipationsMerged is called inside the merge tx to flip
// every non-merged participation to merged. Conflicts are not flipped
// — the merge precondition already rejected those, so any conflict row
// here would be a bug. Returns the number of rows updated.
func (r *Repo) MarkAllParticipationsMerged(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (int64, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE global_branch_participations
		    SET status = 'merged', last_synced_at = now()
		  WHERE global_branch_id = $1 AND status <> 'merged'`,
		branchID,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── Errors specific to the repo layer ──────────────────────────────

// ErrBranchNameConflict signals (tenant_id, name) collision. The
// handler returns 409 with a human readable message.
type ErrBranchNameConflict struct {
	TenantID uuid.UUID
	Name     string
}

func (e *ErrBranchNameConflict) Error() string {
	return fmt.Sprintf("global-branch: branch %q already exists for tenant %s", e.Name, e.TenantID)
}

// ── Scanning ───────────────────────────────────────────────────────

type rowLike interface{ Scan(...any) error }

func scanBranch(r rowLike) (*domain.GlobalBranch, error) {
	v := &domain.GlobalBranch{}
	var (
		status   string
		mergedAt *time.Time
		mergedBy *uuid.UUID
	)
	if err := r.Scan(&v.ID, &v.TenantID, &v.Name, &v.BaseRef, &status, &v.Description,
		&v.CreatedBy, &v.CreatedAt, &mergedAt, &mergedBy); err != nil {
		return nil, err
	}
	v.Status = domain.BranchStatus(status)
	v.MergedAt = mergedAt
	v.MergedBy = mergedBy
	return v, nil
}

func scanParticipation(r rowLike) (*domain.Participation, error) {
	v := &domain.Participation{}
	var (
		status       string
		lastSyncedAt *time.Time
	)
	if err := r.Scan(&v.GlobalBranchID, &v.ServiceName, &v.LocalBranchRef, &status, &lastSyncedAt); err != nil {
		return nil, err
	}
	v.Status = domain.ParticipationStatus(status)
	v.LastSyncedAt = lastSyncedAt
	return v, nil
}
