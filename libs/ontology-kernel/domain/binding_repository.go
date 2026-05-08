// PostgreSQL-backed repository for object type bindings.
//
// Bindings are declarative control-plane rows. Materialised objects
// already flow through ObjectStore; this repository keeps the residual
// PG access out of HTTP handlers while S1 finishes the Cassandra
// runtime migration.
//
// Mirrors `libs/ontology-kernel/src/domain/binding_repository.rs`.

package domain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// BindingRepoError mirrors `enum BindingRepoError` — `Sql(sqlx::Error)`
// or `Decode(String)`. The Go variant uses a typed error struct so
// callers can `errors.As` selectively.
type BindingRepoError struct {
	// SQL holds the underlying pgx error when present.
	SQL error
	// Decode holds the verbatim Rust message
	// `failed to decode property_mapping: <error>` produced by the
	// row→binding conversion.
	Decode string
}

// Error mirrors the `Display` impl for `BindingRepoError`.
func (e *BindingRepoError) Error() string {
	if e.SQL != nil {
		return e.SQL.Error()
	}
	return e.Decode
}

// Unwrap exposes the underlying pgx error (so errors.Is(err,
// pgx.ErrNoRows) works on the SQL variant).
func (e *BindingRepoError) Unwrap() error { return e.SQL }

// Constraint mirrors `BindingRepoError::constraint(&self) -> Option<&str>`.
// Returns the PG constraint name when the underlying error is a
// `*pgconn.PgError` carrying a `ConstraintName`, otherwise empty.
func (e *BindingRepoError) Constraint() string {
	var pgErr *pgconn.PgError
	if errors.As(e.SQL, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// CreateBindingInput mirrors `struct CreateBindingInput<'a>`.
type CreateBindingInput struct {
	ID               uuid.UUID
	ObjectTypeID     uuid.UUID
	DatasetID        uuid.UUID
	DatasetBranch    *string
	DatasetVersion   *int32
	PrimaryKeyColumn string
	PropertyMapping  json.RawMessage
	SyncMode         models.ObjectTypeBindingSyncMode
	DefaultMarking   string
	PreviewLimit     int32
	OwnerID          uuid.UUID
}

// UpdateBindingInput mirrors `struct UpdateBindingInput<'a>`.
type UpdateBindingInput struct {
	BindingID        uuid.UUID
	DatasetBranch    *string
	DatasetVersion   *int32
	PrimaryKeyColumn string
	PropertyMapping  json.RawMessage
	SyncMode         models.ObjectTypeBindingSyncMode
	DefaultMarking   string
	PreviewLimit     int32
}

const objectTypeBindingColumns = `id, object_type_id, dataset_id, dataset_branch, dataset_version,
                  primary_key_column, property_mapping, sync_mode, default_marking,
                  preview_limit, owner_id, last_materialized_at, last_run_status,
                  last_run_summary, created_at, updated_at`

// LoadBinding mirrors `load_binding`. Returns nil when the (id,
// object_type_id) pair is unknown.
func LoadBinding(ctx context.Context, db *pgxpool.Pool, objectTypeID, bindingID uuid.UUID) (*models.ObjectTypeBinding, *BindingRepoError) {
	row := db.QueryRow(ctx,
		`SELECT `+objectTypeBindingColumns+`
           FROM object_type_bindings
           WHERE id = $1 AND object_type_id = $2`,
		bindingID, objectTypeID,
	)
	r, err := scanObjectTypeBindingRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, &BindingRepoError{SQL: err}
	}
	binding, decodeErr := r.IntoBinding()
	if decodeErr != nil {
		return nil, &BindingRepoError{Decode: decodeErr.Error()}
	}
	return &binding, nil
}

// CreateBinding mirrors `create_binding`. INSERT…RETURNING preserves
// the same column ordering as the Rust source.
func CreateBinding(ctx context.Context, db *pgxpool.Pool, input CreateBindingInput) (*models.ObjectTypeBinding, *BindingRepoError) {
	row := db.QueryRow(ctx,
		`INSERT INTO object_type_bindings (
               id, object_type_id, dataset_id, dataset_branch, dataset_version,
               primary_key_column, property_mapping, sync_mode, default_marking,
               preview_limit, owner_id
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
           RETURNING `+objectTypeBindingColumns,
		input.ID, input.ObjectTypeID, input.DatasetID,
		input.DatasetBranch, input.DatasetVersion, input.PrimaryKeyColumn,
		input.PropertyMapping, input.SyncMode.AsStr(),
		input.DefaultMarking, input.PreviewLimit, input.OwnerID,
	)
	r, err := scanObjectTypeBindingRow(row)
	if err != nil {
		return nil, &BindingRepoError{SQL: err}
	}
	binding, decodeErr := r.IntoBinding()
	if decodeErr != nil {
		return nil, &BindingRepoError{Decode: decodeErr.Error()}
	}
	return &binding, nil
}

// ListBindings mirrors `list_bindings`. Returns the rows for an
// object type, newest first.
func ListBindings(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) ([]models.ObjectTypeBinding, *BindingRepoError) {
	rows, err := db.Query(ctx,
		`SELECT `+objectTypeBindingColumns+`
           FROM object_type_bindings
           WHERE object_type_id = $1
           ORDER BY created_at DESC`,
		objectTypeID,
	)
	if err != nil {
		return nil, &BindingRepoError{SQL: err}
	}
	defer rows.Close()
	out := []models.ObjectTypeBinding{}
	for rows.Next() {
		r, err := scanObjectTypeBindingRow(rows)
		if err != nil {
			return nil, &BindingRepoError{SQL: err}
		}
		binding, decodeErr := r.IntoBinding()
		if decodeErr != nil {
			return nil, &BindingRepoError{Decode: decodeErr.Error()}
		}
		out = append(out, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, &BindingRepoError{SQL: err}
	}
	return out, nil
}

// UpdateBinding mirrors `update_binding`. UPDATE…RETURNING preserves
// the column ordering. PrimaryKeyColumn / PropertyMapping /
// SyncMode / DefaultMarking / PreviewLimit always replace; the
// dataset_branch & dataset_version follow `Option<&str>` /
// `Option<i32>` semantics — the caller decides what to pass.
func UpdateBinding(ctx context.Context, db *pgxpool.Pool, input UpdateBindingInput) (*models.ObjectTypeBinding, *BindingRepoError) {
	row := db.QueryRow(ctx,
		`UPDATE object_type_bindings
           SET dataset_branch = $2,
               dataset_version = $3,
               primary_key_column = $4,
               property_mapping = $5,
               sync_mode = $6,
               default_marking = $7,
               preview_limit = $8,
               updated_at = NOW()
           WHERE id = $1
           RETURNING `+objectTypeBindingColumns,
		input.BindingID,
		input.DatasetBranch, input.DatasetVersion, input.PrimaryKeyColumn,
		input.PropertyMapping, input.SyncMode.AsStr(),
		input.DefaultMarking, input.PreviewLimit,
	)
	r, err := scanObjectTypeBindingRow(row)
	if err != nil {
		return nil, &BindingRepoError{SQL: err}
	}
	binding, decodeErr := r.IntoBinding()
	if decodeErr != nil {
		return nil, &BindingRepoError{Decode: decodeErr.Error()}
	}
	return &binding, nil
}

// DeleteBinding mirrors `delete_binding`. Returns true when the row
// existed.
func DeleteBinding(ctx context.Context, db *pgxpool.Pool, objectTypeID, bindingID uuid.UUID) (bool, *BindingRepoError) {
	tag, err := db.Exec(ctx,
		`DELETE FROM object_type_bindings WHERE id = $1 AND object_type_id = $2`,
		bindingID, objectTypeID,
	)
	if err != nil {
		return false, &BindingRepoError{SQL: err}
	}
	return tag.RowsAffected() > 0, nil
}

// RecordMaterializationResult mirrors `record_materialization_result`.
// Updates last_materialized_at + last_run_status + last_run_summary.
func RecordMaterializationResult(ctx context.Context, db *pgxpool.Pool, bindingID uuid.UUID, status string, summary json.RawMessage) *BindingRepoError {
	if _, err := db.Exec(ctx,
		`UPDATE object_type_bindings
           SET last_materialized_at = NOW(),
               last_run_status = $2,
               last_run_summary = $3,
               updated_at = NOW()
           WHERE id = $1`,
		bindingID, status, summary,
	); err != nil {
		return &BindingRepoError{SQL: err}
	}
	return nil
}

// scanObjectTypeBindingRow scans into the persisted row shape; the
// caller then converts to the public model via IntoBinding().
func scanObjectTypeBindingRow(row scanRow) (models.ObjectTypeBindingRow, error) {
	var r models.ObjectTypeBindingRow
	if err := row.Scan(
		&r.ID, &r.ObjectTypeID, &r.DatasetID,
		&r.DatasetBranch, &r.DatasetVersion, &r.PrimaryKeyColumn,
		&r.PropertyMapping, &r.SyncMode, &r.DefaultMarking,
		&r.PreviewLimit, &r.OwnerID,
		&r.LastMaterializedAt, &r.LastRunStatus, &r.LastRunSummary,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return models.ObjectTypeBindingRow{}, err
	}
	return r, nil
}

// Compile-time pin: the constructed BindingRepoError must implement error.
var _ error = (*BindingRepoError)(nil)
