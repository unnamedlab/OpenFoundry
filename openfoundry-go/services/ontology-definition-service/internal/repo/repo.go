// Package repo holds SQL queries + embedded migration for
// ontology-definition-service.
//
// All queries are schema-qualified to ontology_schema (matches the
// Rust impl which sets search_path on the pool at connect time).
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

	"github.com/openfoundry/openfoundry-go/services/ontology-definition-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

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

type Repo struct{ Pool *pgxpool.Pool }

const objectTypeSelect = `SELECT id, name, display_name, description,
	primary_key_property, icon, color, owner_id, created_at, updated_at
	FROM ontology_schema.object_types`

func (r *Repo) ListObjectTypes(ctx context.Context) ([]models.ObjectType, error) {
	rows, err := r.Pool.Query(ctx, objectTypeSelect+` ORDER BY name LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ObjectType, 0)
	for rows.Next() {
		v, err := scanObjectType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetObjectType(ctx context.Context, id uuid.UUID) (*models.ObjectType, error) {
	row := r.Pool.QueryRow(ctx, objectTypeSelect+` WHERE id = $1`, id)
	v, err := scanObjectType(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateObjectType(ctx context.Context, body *models.CreateObjectTypeRequest, ownerID uuid.UUID) (*models.ObjectType, error) {
	id := uuid.New()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO ontology_schema.object_types
		    (id, name, display_name, description, primary_key_property,
		     icon, color, owner_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, name, display_name, description, primary_key_property,
		           icon, color, owner_id, created_at, updated_at`,
		id, strings.TrimSpace(body.Name), body.DisplayName, body.Description,
		body.PrimaryKeyProperty, body.Icon, body.Color, ownerID,
	)
	return scanObjectType(row)
}

func (r *Repo) UpdateObjectType(ctx context.Context, id uuid.UUID, body *models.UpdateObjectTypeRequest) (*models.ObjectType, error) {
	current, err := r.GetObjectType(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	dn := current.DisplayName
	if body.DisplayName != nil {
		dn = *body.DisplayName
	}
	desc := current.Description
	if body.Description != nil {
		desc = *body.Description
	}
	pk := current.PrimaryKeyProperty
	if body.PrimaryKeyProperty != nil {
		pk = body.PrimaryKeyProperty
	}
	icon := current.Icon
	if body.Icon != nil {
		icon = body.Icon
	}
	color := current.Color
	if body.Color != nil {
		color = body.Color
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE ontology_schema.object_types SET
		    display_name = $2, description = $3, primary_key_property = $4,
		    icon = $5, color = $6, updated_at = $7
		  WHERE id = $1
		  RETURNING id, name, display_name, description, primary_key_property,
		            icon, color, owner_id, created_at, updated_at`,
		id, dn, desc, pk, icon, color, time.Now().UTC(),
	)
	return scanObjectType(row)
}

func (r *Repo) DeleteObjectType(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM ontology_schema.object_types WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

type rowLikeT interface{ Scan(...any) error }

func scanObjectType(r rowLikeT) (*models.ObjectType, error) {
	v := &models.ObjectType{}
	if err := r.Scan(&v.ID, &v.Name, &v.DisplayName, &v.Description,
		&v.PrimaryKeyProperty, &v.Icon, &v.Color, &v.OwnerID,
		&v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}
