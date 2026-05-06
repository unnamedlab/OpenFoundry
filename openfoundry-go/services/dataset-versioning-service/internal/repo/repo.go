// Package repo holds SQL queries + embedded migrations for
// dataset-versioning-service.
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

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration in lex order. Idempotent.
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

// Repo wraps the SQL surface for the foundation slice (datasets).
type Repo struct{ Pool *pgxpool.Pool }

const datasetSelect = `SELECT id, name, description, format, storage_path,
	size_bytes, row_count, owner_id, tags, current_version, created_at, updated_at
	FROM datasets`

func (r *Repo) ListDatasets(ctx context.Context, ownerID *uuid.UUID, limit int) ([]models.Dataset, error) {
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var (
		rows pgx.Rows
		err  error
	)
	if ownerID != nil {
		rows, err = r.Pool.Query(ctx, datasetSelect+` WHERE owner_id = $1 ORDER BY updated_at DESC LIMIT $2`, *ownerID, limit)
	} else {
		rows, err = r.Pool.Query(ctx, datasetSelect+` ORDER BY updated_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Dataset, 0)
	for rows.Next() {
		v, err := scanDataset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetDataset(ctx context.Context, id uuid.UUID) (*models.Dataset, error) {
	row := r.Pool.QueryRow(ctx, datasetSelect+` WHERE id = $1`, id)
	v, err := scanDataset(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateDataset(ctx context.Context, body *models.CreateDatasetRequest, ownerID uuid.UUID) (*models.Dataset, error) {
	id := uuid.New()
	format := "parquet"
	if body.Format != nil && *body.Format != "" {
		format = *body.Format
	}
	tags := body.Tags
	if tags == nil {
		tags = []string{}
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO datasets
		    (id, name, description, format, storage_path, owner_id, tags)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, name, description, format, storage_path, size_bytes,
		           row_count, owner_id, tags, current_version, created_at, updated_at`,
		id, strings.TrimSpace(body.Name), body.Description, format,
		strings.TrimSpace(body.StoragePath), ownerID, tags,
	)
	return scanDataset(row)
}

func (r *Repo) UpdateDataset(ctx context.Context, id uuid.UUID, body *models.UpdateDatasetRequest) (*models.Dataset, error) {
	current, err := r.GetDataset(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	desc := current.Description
	if body.Description != nil {
		desc = *body.Description
	}
	tags := current.Tags
	if body.Tags != nil {
		tags = body.Tags
	}
	size := current.SizeBytes
	if body.SizeBytes != nil {
		size = *body.SizeBytes
	}
	rowCount := current.RowCount
	if body.RowCount != nil {
		rowCount = *body.RowCount
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE datasets SET
		    description = $2, tags = $3, size_bytes = $4, row_count = $5,
		    updated_at = $6
		  WHERE id = $1
		  RETURNING id, name, description, format, storage_path, size_bytes,
		            row_count, owner_id, tags, current_version, created_at, updated_at`,
		id, desc, tags, size, rowCount, time.Now().UTC(),
	)
	return scanDataset(row)
}

func (r *Repo) DeleteDataset(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM datasets WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

type rowLikeT interface{ Scan(...any) error }

func scanDataset(r rowLikeT) (*models.Dataset, error) {
	v := &models.Dataset{}
	if err := r.Scan(&v.ID, &v.Name, &v.Description, &v.Format, &v.StoragePath,
		&v.SizeBytes, &v.RowCount, &v.OwnerID, &v.Tags, &v.CurrentVersion,
		&v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	return v, nil
}
