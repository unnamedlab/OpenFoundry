// Package repo holds SQL queries + embedded migrations for
// authorization-policy-service.
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
