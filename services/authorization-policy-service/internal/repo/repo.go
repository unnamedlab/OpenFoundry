// Package repo holds SQL queries + embedded migrations for
// authorization-policy-service.
package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration in lex order. Idempotent —
// CREATE TABLE IF NOT EXISTS.
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

// DB is the pgx surface used by Repo; *pgxpool.Pool and pgxmock pools
// both satisfy it. Exists so repo logic (notably tenant filtering on
// abac_policies) can be unit-tested without spinning up Postgres.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Repo wraps the cedar_policies SQL surface.
type Repo struct{ Pool DB }

const cedarSelect = `SELECT id, tenant_id, version, source, description, active,
	created_by, created_at, updated_at FROM cedar_policies`

// Tenant scoping for cedar_policies:
//
//   - Reads (List/Get): a non-nil tenantID returns the tenant's own rows
//     plus any platform-global rows (tenant_id IS NULL). A nil tenantID
//     denotes a platform/admin caller and returns only global rows so
//     cross-tenant data never leaks through the absence of an org claim.
//   - Writes (Create/Update/Delete): match the row's tenant_id exactly
//     against the caller's sealed tenant. A tenant caller can never
//     touch a global row, and vice versa.

// ListCedarPolicies returns rows visible to `tenantID`, ordered
// most-recent-first. See package comment for the scoping rules.
func (r *Repo) ListCedarPolicies(ctx context.Context, tenantID *uuid.UUID) ([]models.CedarPolicy, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if tenantID == nil {
		rows, err = r.Pool.Query(ctx,
			cedarSelect+` WHERE tenant_id IS NULL ORDER BY updated_at DESC LIMIT 500`)
	} else {
		rows, err = r.Pool.Query(ctx,
			cedarSelect+` WHERE tenant_id = $1 OR tenant_id IS NULL
			 ORDER BY updated_at DESC LIMIT 500`, *tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CedarPolicy, 0)
	for rows.Next() {
		p, err := scanCedarPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetCedarPolicy returns one row by id, scoped to `tenantID` per the
// read rules above. Returns (nil, nil) on no row.
func (r *Repo) GetCedarPolicy(ctx context.Context, tenantID *uuid.UUID, id string) (*models.CedarPolicy, error) {
	var row pgx.Row
	if tenantID == nil {
		row = r.Pool.QueryRow(ctx,
			cedarSelect+` WHERE id = $1 AND tenant_id IS NULL`, id)
	} else {
		row = r.Pool.QueryRow(ctx,
			cedarSelect+` WHERE id = $1 AND (tenant_id = $2 OR tenant_id IS NULL)`,
			id, *tenantID)
	}
	p, err := scanCedarPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// CreateCedarPolicy inserts a new row stamped with `tenantID` (sealed
// from the caller's JWT). The caller MUST have already validated
// `body.Source` against the Cedar schema — repo trusts the input.
func (r *Repo) CreateCedarPolicy(ctx context.Context, body *models.CreateCedarPolicyRequest, callerID uuid.UUID, tenantID *uuid.UUID) (*models.CedarPolicy, error) {
	version := int32(1)
	if body.Version != nil && *body.Version > 0 {
		version = *body.Version
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	now := time.Now().UTC()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO cedar_policies
		    (id, tenant_id, version, source, description, active, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		 RETURNING id, tenant_id, version, source, description, active, created_by, created_at, updated_at`,
		strings.TrimSpace(body.ID), tenantID, version, body.Source, body.Description,
		active, callerID, now,
	)
	return scanCedarPolicy(row)
}

// UpdateCedarPolicy applies a partial patch to a row owned by `tenantID`
// and bumps version on source changes. Returns (nil, nil) when no row
// matches both `id` and the caller's tenant boundary — callers must
// translate that into a 404 (not 403) to avoid leaking the existence
// of another tenant's row.
func (r *Repo) UpdateCedarPolicy(ctx context.Context, tenantID *uuid.UUID, id string, body *models.UpdateCedarPolicyRequest) (*models.CedarPolicy, error) {
	current, err := r.getCedarPolicyForWrite(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	source := current.Source
	version := current.Version
	if body.Source != nil && *body.Source != current.Source {
		source = *body.Source
		version = current.Version + 1
	}
	desc := current.Description
	if body.Description != nil {
		desc = body.Description
	}
	active := current.Active
	if body.Active != nil {
		active = *body.Active
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE cedar_policies SET
		    version = $2, source = $3, description = $4, active = $5,
		    updated_at = $6
		  WHERE id = $1
		  RETURNING id, tenant_id, version, source, description, active, created_by,
		            created_at, updated_at`,
		id, version, source, desc, active, time.Now().UTC(),
	)
	return scanCedarPolicy(row)
}

// DeleteCedarPolicy removes a row owned by `tenantID`. Returns false
// when no row matched the (id, tenantID) pair.
func (r *Repo) DeleteCedarPolicy(ctx context.Context, tenantID *uuid.UUID, id string) (bool, error) {
	var (
		cmd pgconn.CommandTag
		err error
	)
	if tenantID == nil {
		cmd, err = r.Pool.Exec(ctx,
			`DELETE FROM cedar_policies WHERE id = $1 AND tenant_id IS NULL`, id)
	} else {
		cmd, err = r.Pool.Exec(ctx,
			`DELETE FROM cedar_policies WHERE id = $1 AND tenant_id = $2`,
			id, *tenantID)
	}
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// ─── helpers ────────────────────────────────────────────────────────

type rowLikeT interface{ Scan(...any) error }

func scanCedarPolicy(r rowLikeT) (*models.CedarPolicy, error) {
	p := &models.CedarPolicy{}
	if err := r.Scan(&p.ID, &p.TenantID, &p.Version, &p.Source, &p.Description,
		&p.Active, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// getCedarPolicyForWrite returns the policy only when it is owned by the
// caller's tenant. Globals (tenant_id IS NULL) are read-visible but not
// writable from a tenant context — they must be edited by the platform
// admin path (tenantID == nil).
func (r *Repo) getCedarPolicyForWrite(ctx context.Context, tenantID *uuid.UUID, id string) (*models.CedarPolicy, error) {
	var row pgx.Row
	if tenantID == nil {
		row = r.Pool.QueryRow(ctx,
			cedarSelect+` WHERE id = $1 AND tenant_id IS NULL`, id)
	} else {
		row = r.Pool.QueryRow(ctx,
			cedarSelect+` WHERE id = $1 AND tenant_id = $2`, id, *tenantID)
	}
	p, err := scanCedarPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ─── ABAC policies ──────────────────────────────────────────────────

const abacSelect = `SELECT id, tenant_id, name, description, effect, resource, action,
	conditions, row_filter, enabled, created_by, created_at, updated_at
	FROM abac_policies`

// ListABACPolicies returns every policy owned by tenantID. Cross-tenant
// reads are rejected at the SQL layer — there is no admin override.
func (r *Repo) ListABACPolicies(ctx context.Context, tenantID uuid.UUID) ([]models.ABACPolicy, error) {
	rows, err := r.Pool.Query(ctx,
		abacSelect+` WHERE tenant_id = $1 ORDER BY updated_at DESC LIMIT 500`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ABACPolicy, 0)
	for rows.Next() {
		p, err := scanABACPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetABACPolicy returns the policy when it exists *and* belongs to
// tenantID. A row that belongs to a different tenant is reported as
// not-found so callers cannot probe other tenants by guessing IDs.
func (r *Repo) GetABACPolicy(ctx context.Context, tenantID, id uuid.UUID) (*models.ABACPolicy, error) {
	row := r.Pool.QueryRow(ctx, abacSelect+` WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	p, err := scanABACPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// CreateABACPolicy inserts a new policy under tenantID. The caller-supplied
// body never carries tenant_id — it is derived from the auth context by
// the handler so a tenant A user cannot author policies for tenant B.
func (r *Repo) CreateABACPolicy(ctx context.Context, body *models.CreateABACPolicyRequest, tenantID, callerID uuid.UUID) (*models.ABACPolicy, error) {
	id := uuid.New()
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	conds := body.Conditions
	if len(conds) == 0 {
		conds = json.RawMessage(`{}`)
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO abac_policies
		    (id, tenant_id, name, description, effect, resource, action, conditions, row_filter, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, tenant_id, name, description, effect, resource, action, conditions,
		           row_filter, enabled, created_by, created_at, updated_at`,
		id, tenantID, strings.TrimSpace(body.Name), body.Description,
		body.Effect, strings.TrimSpace(body.Resource), strings.TrimSpace(body.Action),
		conds, body.RowFilter, enabled, callerID,
	)
	return scanABACPolicy(row)
}

func (r *Repo) UpdateABACPolicy(ctx context.Context, tenantID, id uuid.UUID, body *models.UpdateABACPolicyRequest) (*models.ABACPolicy, error) {
	current, err := r.GetABACPolicy(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	desc := current.Description
	if body.Description != nil {
		desc = body.Description
	}
	effect := current.Effect
	if body.Effect != nil {
		effect = *body.Effect
	}
	resource := current.Resource
	if body.Resource != nil {
		resource = *body.Resource
	}
	action := current.Action
	if body.Action != nil {
		action = *body.Action
	}
	conds := current.Conditions
	if len(body.Conditions) > 0 {
		conds = body.Conditions
	}
	rowFilter := current.RowFilter
	if body.RowFilter != nil {
		rowFilter = body.RowFilter
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE abac_policies SET
		    description = $3, effect = $4, resource = $5, action = $6,
		    conditions = $7, row_filter = $8, enabled = $9, updated_at = $10
		  WHERE id = $1 AND tenant_id = $2
		  RETURNING id, tenant_id, name, description, effect, resource, action, conditions,
		            row_filter, enabled, created_by, created_at, updated_at`,
		id, tenantID, desc, effect, resource, action, conds, rowFilter, enabled, time.Now().UTC(),
	)
	return scanABACPolicy(row)
}

func (r *Repo) DeleteABACPolicy(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx,
		`DELETE FROM abac_policies WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanABACPolicy(r rowLikeT) (*models.ABACPolicy, error) {
	p := &models.ABACPolicy{}
	if err := r.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.Effect, &p.Resource,
		&p.Action, &p.Conditions, &p.RowFilter, &p.Enabled,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ListEnabledABACPoliciesMatching returns enabled abac_policies owned by
// tenantID and matching (resource, action) — wildcards `*` accepted on
// either side. Tenant filtering is mandatory: passing uuid.Nil collapses
// every tenant's policies into one bucket and is treated as a caller bug.
func (r *Repo) ListEnabledABACPoliciesMatching(ctx context.Context, tenantID uuid.UUID, resource, action string) ([]models.ABACPolicy, error) {
	rows, err := r.Pool.Query(ctx,
		abacSelect+`
		WHERE tenant_id = $1
		  AND enabled = TRUE
		  AND (resource = $2 OR resource = '*')
		  AND (action = $3 OR action = '*')
		ORDER BY created_at ASC`,
		tenantID, resource, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ABACPolicy, 0)
	for rows.Next() {
		p, err := scanABACPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ─── Restricted views (read-only cross-service) ─────────────────────

// RestrictedView mirrors the restricted_views row read by the ABAC
// evaluator. The table is owned operationally by
// identity-federation-service (slice 7a CRUD); we read it here only.
type RestrictedView struct {
	ID                  uuid.UUID
	Name                string
	Resource            string
	Action              string
	Conditions          json.RawMessage
	RowFilter           *string
	HiddenColumns       []string
	MarkingColumns      []string
	AllowedOrgIDs       []uuid.UUID
	AllowedMarkings     []string
	ConsumerModeEnabled bool
	AllowGuestAccess    bool
}

// ListEnabledRestrictedViewsMatching returns enabled restricted_views
// matching (resource, action) — wildcards `*` accepted.
func (r *Repo) ListEnabledRestrictedViewsMatching(ctx context.Context, resource, action string) ([]RestrictedView, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, name, resource, action, conditions, row_filter,
		        hidden_columns, marking_columns, allowed_org_ids, allowed_markings,
		        consumer_mode_enabled, allow_guest_access
		   FROM restricted_views
		  WHERE enabled = TRUE
		    AND (resource = $1 OR resource = '*')
		    AND (action = $2 OR action = '*')
		  ORDER BY created_at ASC`,
		resource, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RestrictedView, 0)
	for rows.Next() {
		var (
			v               RestrictedView
			hiddenJSON      []byte
			markingColsJSON []byte
			allowedOrgJSON  []byte
			allowedMarkJSON []byte
		)
		if err := rows.Scan(&v.ID, &v.Name, &v.Resource, &v.Action,
			&v.Conditions, &v.RowFilter,
			&hiddenJSON, &markingColsJSON, &allowedOrgJSON, &allowedMarkJSON,
			&v.ConsumerModeEnabled, &v.AllowGuestAccess); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(hiddenJSON, &v.HiddenColumns)
		_ = json.Unmarshal(markingColsJSON, &v.MarkingColumns)
		_ = json.Unmarshal(allowedOrgJSON, &v.AllowedOrgIDs)
		_ = json.Unmarshal(allowedMarkJSON, &v.AllowedMarkings)
		out = append(out, v)
	}
	return out, rows.Err()
}
