// Control-plane definition lookups shared by ontology handlers.
//
// Mirrors `libs/ontology-kernel/src/domain/definition_queries.rs`.
// Declarative ontology metadata stays on PostgreSQL during the
// migration to Cassandra; this module centralises every remaining
// PG-backed lookup so handlers consume typed Go functions instead of
// embedding raw `pgx` queries inline.

package domain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

const (
	actionTypeColumns = `id, name, display_name, description, object_type_id, operation_kind,
                  input_schema, form_schema, config, confirmation_required, permission_key, authorization_policy,
                  owner_id,
                  created_at, updated_at`
)

// LoadActionsForObjectType mirrors `load_actions_for_object_type`.
// Returns every action_types row bound to the given object type,
// newest first.
func LoadActionsForObjectType(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) ([]models.ActionTypeRow, error) {
	rows, err := db.Query(ctx,
		`SELECT `+actionTypeColumns+` FROM action_types WHERE object_type_id = $1 ORDER BY created_at DESC`,
		objectTypeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ActionTypeRow{}
	for rows.Next() {
		row, err := scanActionTypeRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// LoadActionType mirrors `load_action_type`. Returns nil when the
// action_id does not exist (Rust `Option::None` ↔ Go `nil`).
func LoadActionType(ctx context.Context, db *pgxpool.Pool, actionID uuid.UUID) (*models.ActionTypeRow, error) {
	row := db.QueryRow(ctx,
		`SELECT `+actionTypeColumns+` FROM action_types WHERE id = $1`,
		actionID,
	)
	rec, err := scanActionTypeRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// LoadPropertyForObjectType mirrors `load_property_for_object_type`.
// Returns nil when the property either does not exist or is bound to
// a different object type.
func LoadPropertyForObjectType(ctx context.Context, db *pgxpool.Pool, objectTypeID, propertyID uuid.UUID) (*models.Property, error) {
	row := db.QueryRow(ctx,
		`SELECT id, object_type_id, name, display_name, description, property_type, required,
                  unique_constraint, time_dependent, default_value, validation_rules,
                  inline_edit_config, created_at, updated_at
           FROM properties
           WHERE id = $1 AND object_type_id = $2`,
		propertyID, objectTypeID,
	)
	var (
		p         models.Property
		inlineRaw []byte
	)
	err := row.Scan(
		&p.ID, &p.ObjectTypeID, &p.Name, &p.DisplayName, &p.Description, &p.PropertyType,
		&p.Required, &p.UniqueConstraint, &p.TimeDependent, &p.DefaultValue, &p.ValidationRules,
		&inlineRaw, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(inlineRaw) > 0 {
		var cfg models.PropertyInlineEditConfig
		if err := json.Unmarshal(inlineRaw, &cfg); err == nil {
			p.InlineEditConfig = &cfg
		}
	}
	return &p, nil
}

// ObjectTypeExists mirrors `object_type_exists`.
func ObjectTypeExists(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM object_types WHERE id = $1)`,
		objectTypeID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// LoadObjectType mirrors `load_object_type`.
func LoadObjectType(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) (*models.ObjectType, error) {
	row := db.QueryRow(ctx,
		`SELECT id, name, display_name, description, primary_key_property, icon, color, owner_id, created_at, updated_at
           FROM object_types WHERE id = $1`,
		objectTypeID,
	)
	var t models.ObjectType
	err := row.Scan(
		&t.ID, &t.Name, &t.DisplayName, &t.Description,
		&t.PrimaryKeyProperty, &t.Icon, &t.Color,
		&t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// LoadLinkType mirrors `load_link_type`.
func LoadLinkType(ctx context.Context, db *pgxpool.Pool, linkTypeID uuid.UUID) (*models.LinkType, error) {
	row := db.QueryRow(ctx,
		`SELECT id, name, display_name, description, source_type_id, target_type_id, cardinality, owner_id, created_at, updated_at
           FROM link_types WHERE id = $1`,
		linkTypeID,
	)
	var lt models.LinkType
	err := row.Scan(
		&lt.ID, &lt.Name, &lt.DisplayName, &lt.Description,
		&lt.SourceTypeID, &lt.TargetTypeID, &lt.Cardinality,
		&lt.OwnerID, &lt.CreatedAt, &lt.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lt, nil
}

// LoadObjectTypeDisplayName mirrors `load_object_type_display_name`.
// Returns nil when the row does not exist.
func LoadObjectTypeDisplayName(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) (*string, error) {
	var name string
	err := db.QueryRow(ctx,
		`SELECT display_name FROM object_types WHERE id = $1`,
		objectTypeID,
	).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &name, nil
}

// LoadObjectTypesByIDs mirrors `load_object_types_by_ids`. Uses
// PostgreSQL's `ANY($1)` array binding identical to the Rust source.
func LoadObjectTypesByIDs(ctx context.Context, db *pgxpool.Pool, typeIDs []uuid.UUID) ([]models.ObjectType, error) {
	rows, err := db.Query(ctx,
		`SELECT id, name, display_name, description, primary_key_property, icon, color, owner_id, created_at, updated_at
           FROM object_types WHERE id = ANY($1)`,
		typeIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ObjectType{}
	for rows.Next() {
		var t models.ObjectType
		if err := rows.Scan(
			&t.ID, &t.Name, &t.DisplayName, &t.Description,
			&t.PrimaryKeyProperty, &t.Icon, &t.Color,
			&t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scanRow is the minimal interface satisfied by both pgx.Row (single)
// and pgx.Rows (cursor) — keeps the action_types scanner usable from
// both call sites.
type scanRow interface {
	Scan(dest ...any) error
}

func scanActionTypeRow(row scanRow) (models.ActionTypeRow, error) {
	var r models.ActionTypeRow
	if err := row.Scan(
		&r.ID, &r.Name, &r.DisplayName, &r.Description, &r.ObjectTypeID, &r.OperationKind,
		&r.InputSchema, &r.FormSchema, &r.Config,
		&r.ConfirmationRequired, &r.PermissionKey, &r.AuthorizationPolicy,
		&r.OwnerID, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return models.ActionTypeRow{}, err
	}
	return r, nil
}
