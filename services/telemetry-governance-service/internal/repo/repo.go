// Package repo holds SQL queries + embedded migrations for telemetry-governance-service.
//
// The service hosts four sub-features (telemetry_exports, health_checks,
// execution_runs, monitoring_rules), all using the same parent/child
// schema shape. We expose a single FeatureRepo bound to a parent + child
// table pair so the handlers can build one repo per feature without
// duplicating SQL.
package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded `migrations/*.sql` file in lex order.
// Idempotent (CREATE TABLE IF NOT EXISTS).
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

// FeatureRepo wraps the parent + child SQL surface for a single
// sub-feature. Table names are passed in at construction so the same
// implementation serves all four features.
//
// Important: table names are operator-controlled (constants in
// models.AllFeatures), never user input. This sidesteps the "you can't
// parameterise table names in Postgres" trap.
type FeatureRepo struct {
	Pool      *pgxpool.Pool
	Tables    models.FeatureTables
}

// ListPrimary returns the most recent 200 parent rows.
func (r *FeatureRepo) ListPrimary(ctx context.Context) ([]models.PrimaryItem, error) {
	rows, err := r.Pool.Query(ctx,
		fmt.Sprintf(`SELECT id, payload, created_at FROM %s ORDER BY created_at DESC LIMIT 200`, r.Tables.Primary),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.PrimaryItem, 0)
	for rows.Next() {
		var it models.PrimaryItem
		if err := rows.Scan(&it.ID, &it.Payload, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CreatePrimary inserts a new parent row.
func (r *FeatureRepo) CreatePrimary(ctx context.Context, id uuid.UUID, payload json.RawMessage) (*models.PrimaryItem, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	row := r.Pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, payload) VALUES ($1, $2) RETURNING id, payload, created_at`, r.Tables.Primary),
		id, payload,
	)
	it := &models.PrimaryItem{}
	if err := row.Scan(&it.ID, &it.Payload, &it.CreatedAt); err != nil {
		return nil, err
	}
	return it, nil
}

// GetPrimary looks up a parent row by id.
func (r *FeatureRepo) GetPrimary(ctx context.Context, id uuid.UUID) (*models.PrimaryItem, error) {
	row := r.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT id, payload, created_at FROM %s WHERE id = $1`, r.Tables.Primary),
		id,
	)
	it := &models.PrimaryItem{}
	if err := row.Scan(&it.ID, &it.Payload, &it.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return it, nil
}

// ListSecondary returns up to 200 children for a parent.
func (r *FeatureRepo) ListSecondary(ctx context.Context, parentID uuid.UUID) ([]models.SecondaryItem, error) {
	rows, err := r.Pool.Query(ctx,
		fmt.Sprintf(`SELECT id, parent_id, payload, created_at FROM %s WHERE parent_id = $1 ORDER BY created_at DESC LIMIT 200`, r.Tables.Secondary),
		parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SecondaryItem, 0)
	for rows.Next() {
		var it models.SecondaryItem
		if err := rows.Scan(&it.ID, &it.ParentID, &it.Payload, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CreateSecondary inserts a new child row.
func (r *FeatureRepo) CreateSecondary(ctx context.Context, id, parentID uuid.UUID, payload json.RawMessage) (*models.SecondaryItem, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	row := r.Pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, parent_id, payload) VALUES ($1, $2, $3) RETURNING id, parent_id, payload, created_at`, r.Tables.Secondary),
		id, parentID, payload,
	)
	it := &models.SecondaryItem{}
	if err := row.Scan(&it.ID, &it.ParentID, &it.Payload, &it.CreatedAt); err != nil {
		return nil, err
	}
	return it, nil
}
