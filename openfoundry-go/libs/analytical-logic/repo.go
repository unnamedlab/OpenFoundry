package analyticallogic

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound reports that the requested expression / version does not
// exist. Mirrors RepoError::NotFound on the Rust side.
type ErrNotFound struct{ ID uuid.UUID }

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("analytical expression %s not found", e.ID)
}

// ErrDatabase wraps any other database failure. Mirrors
// RepoError::Database on the Rust side and lets callers match on a
// small, stable surface without leaking the full pgx error tree across
// crate boundaries.
type ErrDatabase struct{ Cause error }

func (e *ErrDatabase) Error() string { return "analytical-logic repo: " + e.Cause.Error() }
func (e *ErrDatabase) Unwrap() error { return e.Cause }

// AnalyticalExpressionRepo is the Postgres-backed repository for
// AnalyticalExpression / AnalyticalExpressionVersion. Wraps a
// pgxpool.Pool so callers (e.g. sql-bi-gateway-service) hold the pool
// for their own bounded context and pass the repo per-call. There is
// no global state.
type AnalyticalExpressionRepo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a new repo over an existing connection pool. Cheap;
// just stores the pointer.
func NewRepo(pool *pgxpool.Pool) *AnalyticalExpressionRepo {
	return &AnalyticalExpressionRepo{pool: pool}
}

// Pool returns the underlying connection pool (tests, shutdown).
func (r *AnalyticalExpressionRepo) Pool() *pgxpool.Pool { return r.pool }

// List returns up to `limit` expressions, newest first.
func (r *AnalyticalExpressionRepo) List(ctx context.Context, limit int64) ([]AnalyticalExpression, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, payload, created_at, updated_at
		   FROM analytical_expressions
		  ORDER BY created_at DESC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, &ErrDatabase{Cause: err}
	}
	defer rows.Close()

	var out []AnalyticalExpression
	for rows.Next() {
		var row AnalyticalExpression
		if err := rows.Scan(&row.ID, &row.Payload, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, &ErrDatabase{Cause: err}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, &ErrDatabase{Cause: err}
	}
	return out, nil
}

// Get fetches a single expression by id. Returns *ErrNotFound when the
// row is missing.
func (r *AnalyticalExpressionRepo) Get(ctx context.Context, id uuid.UUID) (AnalyticalExpression, error) {
	var row AnalyticalExpression
	err := r.pool.QueryRow(ctx,
		`SELECT id, payload, created_at, updated_at
		   FROM analytical_expressions
		  WHERE id = $1`,
		id,
	).Scan(&row.ID, &row.Payload, &row.CreatedAt, &row.UpdatedAt)
	switch {
	case err == nil:
		return row, nil
	case errors.Is(err, pgx.ErrNoRows):
		return AnalyticalExpression{}, &ErrNotFound{ID: id}
	default:
		return AnalyticalExpression{}, &ErrDatabase{Cause: err}
	}
}

// Create inserts a new expression. The id is generated server-side
// (uuid v7) so callers don't have to coordinate.
func (r *AnalyticalExpressionRepo) Create(ctx context.Context, in NewExpression) (AnalyticalExpression, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return AnalyticalExpression{}, &ErrDatabase{Cause: err}
	}
	var row AnalyticalExpression
	err = r.pool.QueryRow(ctx,
		`INSERT INTO analytical_expressions (id, payload) VALUES ($1, $2)
		 RETURNING id, payload, created_at, updated_at`,
		id, in.Payload,
	).Scan(&row.ID, &row.Payload, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return AnalyticalExpression{}, &ErrDatabase{Cause: err}
	}
	return row, nil
}

// ListVersions returns the version history of `parentID`, newest first,
// up to `limit`.
func (r *AnalyticalExpressionRepo) ListVersions(ctx context.Context, parentID uuid.UUID, limit int64) ([]AnalyticalExpressionVersion, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, parent_id, payload, created_at
		   FROM analytical_expression_versions
		  WHERE parent_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		parentID, limit,
	)
	if err != nil {
		return nil, &ErrDatabase{Cause: err}
	}
	defer rows.Close()

	var out []AnalyticalExpressionVersion
	for rows.Next() {
		var row AnalyticalExpressionVersion
		if err := rows.Scan(&row.ID, &row.ParentID, &row.Payload, &row.CreatedAt); err != nil {
			return nil, &ErrDatabase{Cause: err}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, &ErrDatabase{Cause: err}
	}
	return out, nil
}

// AddVersion appends a new version to an existing expression.
func (r *AnalyticalExpressionRepo) AddVersion(ctx context.Context, parentID uuid.UUID, in NewExpressionVersion) (AnalyticalExpressionVersion, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return AnalyticalExpressionVersion{}, &ErrDatabase{Cause: err}
	}
	var row AnalyticalExpressionVersion
	err = r.pool.QueryRow(ctx,
		`INSERT INTO analytical_expression_versions (id, parent_id, payload)
		 VALUES ($1, $2, $3)
		 RETURNING id, parent_id, payload, created_at`,
		id, parentID, in.Payload,
	).Scan(&row.ID, &row.ParentID, &row.Payload, &row.CreatedAt)
	if err != nil {
		return AnalyticalExpressionVersion{}, &ErrDatabase{Cause: err}
	}
	return row, nil
}
