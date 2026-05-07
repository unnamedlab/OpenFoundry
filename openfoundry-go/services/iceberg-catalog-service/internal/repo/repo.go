// Package repo holds SQL queries + embedded migrations for
// iceberg-catalog-service.
package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
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

const namespaceSelect = `SELECT id, project_rid, name, parent_namespace_id,
	properties, created_at, created_by FROM iceberg_namespaces`

func (r *Repo) ListNamespaces(ctx context.Context, projectRID string) ([]models.IcebergNamespace, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if projectRID != "" {
		rows, err = r.Pool.Query(ctx, namespaceSelect+` WHERE project_rid = $1 ORDER BY name LIMIT 500`, projectRID)
	} else {
		rows, err = r.Pool.Query(ctx, namespaceSelect+` ORDER BY project_rid, name LIMIT 500`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.IcebergNamespace, 0)
	for rows.Next() {
		v, err := scanNamespace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetNamespace(ctx context.Context, id uuid.UUID) (*models.IcebergNamespace, error) {
	row := r.Pool.QueryRow(ctx, namespaceSelect+` WHERE id = $1`, id)
	v, err := scanNamespace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateNamespace(ctx context.Context, body *models.CreateNamespaceRequest, createdBy uuid.UUID) (*models.IcebergNamespace, error) {
	id := uuid.New()
	props := body.Properties
	if len(props) == 0 {
		props = []byte(`{}`)
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO iceberg_namespaces
		    (id, project_rid, name, parent_namespace_id, properties, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, project_rid, name, parent_namespace_id, properties,
		           created_at, created_by`,
		id, body.ProjectRID, body.Name, body.ParentNamespaceID, props, createdBy,
	)
	return scanNamespace(row)
}

func (r *Repo) UpdateNamespaceProperties(ctx context.Context, id uuid.UUID, properties []byte) (*models.IcebergNamespace, error) {
	current, err := r.GetNamespace(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	if len(properties) == 0 {
		return current, nil
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE iceberg_namespaces SET properties = $2 WHERE id = $1
		 RETURNING id, project_rid, name, parent_namespace_id, properties,
		           created_at, created_by`,
		id, properties)
	return scanNamespace(row)
}

func (r *Repo) DeleteNamespace(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM iceberg_namespaces WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

type rowLikeT interface{ Scan(...any) error }

func scanNamespace(r rowLikeT) (*models.IcebergNamespace, error) {
	v := &models.IcebergNamespace{}
	if err := r.Scan(&v.ID, &v.ProjectRID, &v.Name, &v.ParentNamespaceID,
		&v.Properties, &v.CreatedAt, &v.CreatedBy); err != nil {
		return nil, err
	}
	return v, nil
}
