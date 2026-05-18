// Package repo owns the saved-queries Postgres surface for the SQL/BI
// gateway side router: sentinel errors, embedded migrations, and the
// Repo interface consumed by [internal/handler].
//
// Warehousing and tabular handlers still hit pgx directly; they were
// ported from Rust 1:1 and are out of scope for this slice.
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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration in lex order. Idempotent —
// every statement uses CREATE … IF NOT EXISTS so re-running is a no-op.
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

// Sentinel errors returned by Repo implementations. Handlers translate
// them to HTTP status codes.
var (
	ErrNotFound   = errors.New("saved query not found")
	ErrValidation = errors.New("validation failed")
)

// DB is the pgx subset used by SavedQueriesRepo; both *pgxpool.Pool
// and the pgxmock pool satisfy it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// SavedQueries is the persistence surface used by the HTTP handlers.
// Implemented by [PgSavedQueries] in production and a fake in tests.
type SavedQueries interface {
	Create(ctx context.Context, in models.SavedQuery) (models.SavedQuery, error)
	List(ctx context.Context, search string, limit, offset int64) ([]models.SavedQuery, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// PgSavedQueries is the pgx-backed [SavedQueries] implementation.
type PgSavedQueries struct{ Pool DB }

// NewSavedQueries returns a [SavedQueries] backed by pool.
func NewSavedQueries(pool DB) *PgSavedQueries { return &PgSavedQueries{Pool: pool} }

const savedQuerySelect = `SELECT id, name, description, sql, owner_id, created_at, updated_at`

// Create inserts a saved query. The id and owner_id MUST be populated
// by the caller (handler derives owner_id from the JWT claims).
func (r *PgSavedQueries) Create(ctx context.Context, in models.SavedQuery) (models.SavedQuery, error) {
	if in.Name == "" {
		return models.SavedQuery{}, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if in.SQL == "" {
		return models.SavedQuery{}, fmt.Errorf("%w: sql is required", ErrValidation)
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	row := r.Pool.QueryRow(ctx, `
        INSERT INTO saved_queries (id, name, description, sql, owner_id)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING `+savedQuerySelectCols(),
		in.ID, in.Name, in.Description, in.SQL, in.OwnerID)
	return scanSavedQuery(row)
}

// List returns saved queries matching `search` (ILIKE on name or
// description; empty string returns every row), ordered by most-recent
// update, with offset+limit pagination.
func (r *PgSavedQueries) List(ctx context.Context, search string, limit, offset int64) ([]models.SavedQuery, error) {
	var searchVal any
	if search != "" {
		searchVal = "%" + search + "%"
	}
	rows, err := r.Pool.Query(ctx, savedQuerySelect+`
        FROM saved_queries
        WHERE ($1::TEXT IS NULL OR name ILIKE $1 OR description ILIKE $1)
        ORDER BY updated_at DESC
        LIMIT $2 OFFSET $3`, searchVal, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SavedQuery, 0)
	for rows.Next() {
		q, err := scanSavedQuery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Delete removes a saved query by id. Returns [ErrNotFound] when no row
// matches.
func (r *PgSavedQueries) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM saved_queries WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func savedQuerySelectCols() string {
	return `id, name, description, sql, owner_id, created_at, updated_at`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSavedQuery(s rowScanner) (models.SavedQuery, error) {
	var q models.SavedQuery
	if err := s.Scan(&q.ID, &q.Name, &q.Description, &q.SQL, &q.OwnerID,
		&q.CreatedAt, &q.UpdatedAt); err != nil {
		return models.SavedQuery{}, err
	}
	return q, nil
}
