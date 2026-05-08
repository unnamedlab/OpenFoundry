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

// Repo wraps the cedar_policies SQL surface.
type Repo struct{ Pool *pgxpool.Pool }

const cedarSelect = `SELECT id, version, source, description, active,
	created_by, created_at, updated_at FROM cedar_policies`

// ListCedarPolicies returns every row, ordered most-recent-first.
func (r *Repo) ListCedarPolicies(ctx context.Context) ([]models.CedarPolicy, error) {
	rows, err := r.Pool.Query(ctx, cedarSelect+` ORDER BY updated_at DESC LIMIT 500`)
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

// GetCedarPolicy returns one row by id. Returns (nil, nil) on no row.
func (r *Repo) GetCedarPolicy(ctx context.Context, id string) (*models.CedarPolicy, error) {
	row := r.Pool.QueryRow(ctx, cedarSelect+` WHERE id = $1`, id)
	p, err := scanCedarPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// CreateCedarPolicy inserts a new row. The caller MUST have already
// validated `body.Source` against the Cedar schema — repo trusts the input.
func (r *Repo) CreateCedarPolicy(ctx context.Context, body *models.CreateCedarPolicyRequest, callerID uuid.UUID) (*models.CedarPolicy, error) {
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
		    (id, version, source, description, active, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		 RETURNING id, version, source, description, active, created_by, created_at, updated_at`,
		strings.TrimSpace(body.ID), version, body.Source, body.Description,
		active, callerID, now,
	)
	return scanCedarPolicy(row)
}

// UpdateCedarPolicy applies a partial patch and bumps version on source
// changes. Returns (nil, nil) when the row doesn't exist.
func (r *Repo) UpdateCedarPolicy(ctx context.Context, id string, body *models.UpdateCedarPolicyRequest) (*models.CedarPolicy, error) {
	current, err := r.GetCedarPolicy(ctx, id)
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
		  RETURNING id, version, source, description, active, created_by,
		            created_at, updated_at`,
		id, version, source, desc, active, time.Now().UTC(),
	)
	return scanCedarPolicy(row)
}

// DeleteCedarPolicy removes a row. Returns false when no row matched.
func (r *Repo) DeleteCedarPolicy(ctx context.Context, id string) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM cedar_policies WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// ─── helpers ────────────────────────────────────────────────────────

type rowLikeT interface{ Scan(...any) error }

func scanCedarPolicy(r rowLikeT) (*models.CedarPolicy, error) {
	p := &models.CedarPolicy{}
	if err := r.Scan(&p.ID, &p.Version, &p.Source, &p.Description,
		&p.Active, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ─── ABAC policies ──────────────────────────────────────────────────

const abacSelect = `SELECT id, name, description, effect, resource, action,
	conditions, row_filter, enabled, created_by, created_at, updated_at
	FROM abac_policies`

func (r *Repo) ListABACPolicies(ctx context.Context) ([]models.ABACPolicy, error) {
	rows, err := r.Pool.Query(ctx, abacSelect+` ORDER BY updated_at DESC LIMIT 500`)
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

func (r *Repo) GetABACPolicy(ctx context.Context, id uuid.UUID) (*models.ABACPolicy, error) {
	row := r.Pool.QueryRow(ctx, abacSelect+` WHERE id = $1`, id)
	p, err := scanABACPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *Repo) CreateABACPolicy(ctx context.Context, body *models.CreateABACPolicyRequest, callerID uuid.UUID) (*models.ABACPolicy, error) {
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
		    (id, name, description, effect, resource, action, conditions, row_filter, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, name, description, effect, resource, action, conditions,
		           row_filter, enabled, created_by, created_at, updated_at`,
		id, strings.TrimSpace(body.Name), body.Description,
		body.Effect, strings.TrimSpace(body.Resource), strings.TrimSpace(body.Action),
		conds, body.RowFilter, enabled, callerID,
	)
	return scanABACPolicy(row)
}

func (r *Repo) UpdateABACPolicy(ctx context.Context, id uuid.UUID, body *models.UpdateABACPolicyRequest) (*models.ABACPolicy, error) {
	current, err := r.GetABACPolicy(ctx, id)
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
		    description = $2, effect = $3, resource = $4, action = $5,
		    conditions = $6, row_filter = $7, enabled = $8, updated_at = $9
		  WHERE id = $1
		  RETURNING id, name, description, effect, resource, action, conditions,
		            row_filter, enabled, created_by, created_at, updated_at`,
		id, desc, effect, resource, action, conds, rowFilter, enabled, time.Now().UTC(),
	)
	return scanABACPolicy(row)
}

func (r *Repo) DeleteABACPolicy(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM abac_policies WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanABACPolicy(r rowLikeT) (*models.ABACPolicy, error) {
	p := &models.ABACPolicy{}
	if err := r.Scan(&p.ID, &p.Name, &p.Description, &p.Effect, &p.Resource,
		&p.Action, &p.Conditions, &p.RowFilter, &p.Enabled,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ListEnabledABACPoliciesMatching returns enabled abac_policies
// matching (resource, action) — wildcards `*` accepted on either side.
// Used by the ABAC evaluator (slice 3).
func (r *Repo) ListEnabledABACPoliciesMatching(ctx context.Context, resource, action string) ([]models.ABACPolicy, error) {
	rows, err := r.Pool.Query(ctx,
		abacSelect+`
		WHERE enabled = TRUE
		  AND (resource = $1 OR resource = '*')
		  AND (action = $2 OR action = '*')
		ORDER BY created_at ASC`,
		resource, action)
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
		        hidden_columns, allowed_org_ids, allowed_markings,
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
			allowedOrgJSON  []byte
			allowedMarkJSON []byte
		)
		if err := rows.Scan(&v.ID, &v.Name, &v.Resource, &v.Action,
			&v.Conditions, &v.RowFilter,
			&hiddenJSON, &allowedOrgJSON, &allowedMarkJSON,
			&v.ConsumerModeEnabled, &v.AllowGuestAccess); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(hiddenJSON, &v.HiddenColumns)
		_ = json.Unmarshal(allowedOrgJSON, &v.AllowedOrgIDs)
		_ = json.Unmarshal(allowedMarkJSON, &v.AllowedMarkings)
		out = append(out, v)
	}
	return out, rows.Err()
}
