// PostgreSQL-backed repository for declarative link type metadata.
//
// Link instances live behind storage-abstraction LinkStore; link type
// definitions remain declarative S1 metadata on PostgreSQL. Handlers
// consume this module instead of embedding inline SQL.
//
// Mirrors `libs/ontology-kernel/src/domain/link_type_repository.rs`.

package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

const linkTypeColumns = `id, name, display_name, description, source_type_id, target_type_id, cardinality, owner_id, created_at, updated_at`

// CreateLinkType mirrors `create`. INSERT…RETURNING is preserved
// byte-for-byte.
func CreateLinkType(
	ctx context.Context,
	db *pgxpool.Pool,
	id, ownerID uuid.UUID,
	body *models.CreateLinkTypeRequest,
	displayName, description, cardinality string,
) (models.LinkType, error) {
	row := db.QueryRow(ctx,
		`INSERT INTO link_types (id, name, display_name, description, source_type_id, target_type_id, cardinality, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
           RETURNING `+linkTypeColumns,
		id, body.Name, displayName, description, body.SourceTypeID, body.TargetTypeID, cardinality, ownerID,
	)
	return scanLinkType(row)
}

// ListLinkTypes mirrors `list`. Returns the rows + total count for the
// optional object_type_id filter; without filter, totals & rows are
// returned across the whole table.
func ListLinkTypes(
	ctx context.Context,
	db *pgxpool.Pool,
	params models.ListLinkTypesQuery,
	limit, offset int64,
) ([]models.LinkType, int64, error) {
	if params.ObjectTypeID != nil {
		objectTypeID := *params.ObjectTypeID
		var total int64
		err := db.QueryRow(ctx,
			`SELECT COUNT(*) FROM link_types WHERE source_type_id = $1 OR target_type_id = $1`,
			objectTypeID,
		).Scan(&total)
		if err != nil {
			return nil, 0, err
		}
		rows, err := db.Query(ctx,
			`SELECT `+linkTypeColumns+` FROM link_types
               WHERE source_type_id = $1 OR target_type_id = $1
               ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			objectTypeID, limit, offset,
		)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		out := []models.LinkType{}
		for rows.Next() {
			lt, err := scanLinkType(rows)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, lt)
		}
		return out, total, rows.Err()
	}

	var total int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM link_types`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(ctx,
		`SELECT `+linkTypeColumns+` FROM link_types ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.LinkType{}
	for rows.Next() {
		lt, err := scanLinkType(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, lt)
	}
	return out, total, rows.Err()
}

// DeleteLinkType mirrors `delete`. Returns true when the row existed.
func DeleteLinkType(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (bool, error) {
	tag, err := db.Exec(ctx, `DELETE FROM link_types WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// LoadLinkTypeByID mirrors `load`. Returns nil when the id is unknown.
func LoadLinkTypeByID(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*models.LinkType, error) {
	row := db.QueryRow(ctx,
		`SELECT `+linkTypeColumns+` FROM link_types WHERE id = $1`,
		id,
	)
	lt, err := scanLinkType(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lt, nil
}

// UpdateLinkType mirrors `update`. The COALESCE pattern preserves the
// existing display_name/description when the request leaves them
// nil. Cardinality always replaces (Rust signature passes a non-Option
// String).
func UpdateLinkType(
	ctx context.Context,
	db *pgxpool.Pool,
	id uuid.UUID,
	body models.UpdateLinkTypeRequest,
	cardinality string,
) (*models.LinkType, error) {
	row := db.QueryRow(ctx,
		`UPDATE link_types
           SET display_name = COALESCE($2, display_name),
               description = COALESCE($3, description),
               cardinality = $4,
               updated_at = NOW()
           WHERE id = $1
           RETURNING `+linkTypeColumns,
		id, body.DisplayName, body.Description, cardinality,
	)
	lt, err := scanLinkType(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lt, nil
}

func scanLinkType(row scanRow) (models.LinkType, error) {
	var lt models.LinkType
	if err := row.Scan(
		&lt.ID, &lt.Name, &lt.DisplayName, &lt.Description,
		&lt.SourceTypeID, &lt.TargetTypeID, &lt.Cardinality,
		&lt.OwnerID, &lt.CreatedAt, &lt.UpdatedAt,
	); err != nil {
		return models.LinkType{}, err
	}
	return lt, nil
}
