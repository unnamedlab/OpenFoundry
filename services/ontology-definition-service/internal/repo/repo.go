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
	primary_key_property, icon, color, owner_id, created_at, updated_at,
	plural_display_name, editable, backing_dataset_id, backing_dataset_rid,
	pipeline_rid, managed_by
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
		if err := r.enrichObjectTypeMetadata(ctx, v); err != nil {
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
	if err != nil {
		return nil, err
	}
	return v, r.enrichObjectTypeMetadata(ctx, v)
}

func (r *Repo) CreateObjectType(ctx context.Context, body *models.CreateObjectTypeRequest, ownerID uuid.UUID) (*models.ObjectType, error) {
	id := uuid.New()
	if body.ID != nil && *body.ID != uuid.Nil {
		id = *body.ID
	}
	editable := false
	if body.Editable != nil {
		editable = *body.Editable
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO ontology_schema.object_types
		    (id, name, display_name, description, primary_key_property,
		     icon, color, owner_id, plural_display_name, editable,
		     backing_dataset_id, backing_dataset_rid, pipeline_rid, managed_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id, name, display_name, description, primary_key_property,
		           icon, color, owner_id, created_at, updated_at,
		           plural_display_name, editable, backing_dataset_id,
		           backing_dataset_rid, pipeline_rid, managed_by`,
		id, strings.TrimSpace(body.Name), body.DisplayName, body.Description,
		body.PrimaryKeyProperty, body.Icon, body.Color, ownerID,
		body.PluralDisplayName, editable, body.BackingDatasetID,
		body.BackingDatasetRID, body.PipelineRID, body.ManagedBy,
	)
	v, err := scanObjectType(row)
	if err != nil {
		return nil, err
	}
	models.EnrichObjectTypeMetadata(v, nil)
	return v, nil
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
	plural := current.PluralDisplayName
	if body.PluralDisplayName != nil {
		plural = body.PluralDisplayName
	}
	editable := current.Editable
	if body.Editable != nil {
		editable = *body.Editable
	}
	backingDatasetID := current.BackingDatasetID
	if body.BackingDatasetID != nil {
		backingDatasetID = body.BackingDatasetID
	}
	backingDatasetRID := current.BackingDatasetRID
	if body.BackingDatasetRID != nil {
		backingDatasetRID = body.BackingDatasetRID
	}
	pipelineRID := current.PipelineRID
	if body.PipelineRID != nil {
		pipelineRID = body.PipelineRID
	}
	managedBy := current.ManagedBy
	if body.ManagedBy != nil {
		managedBy = body.ManagedBy
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE ontology_schema.object_types SET
		    display_name = $2, description = $3, primary_key_property = $4,
		    icon = $5, color = $6, updated_at = $7,
		    plural_display_name = $8, editable = $9, backing_dataset_id = $10,
		    backing_dataset_rid = $11, pipeline_rid = $12, managed_by = $13
		  WHERE id = $1
		  RETURNING id, name, display_name, description, primary_key_property,
		            icon, color, owner_id, created_at, updated_at,
		            plural_display_name, editable, backing_dataset_id,
		            backing_dataset_rid, pipeline_rid, managed_by`,
		id, dn, desc, pk, icon, color, time.Now().UTC(),
		plural, editable, backingDatasetID, backingDatasetRID, pipelineRID, managedBy,
	)
	v, err := scanObjectType(row)
	if err != nil {
		return nil, err
	}
	return v, r.enrichObjectTypeMetadata(ctx, v)
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
		&v.CreatedAt, &v.UpdatedAt, &v.PluralDisplayName, &v.Editable,
		&v.BackingDatasetID, &v.BackingDatasetRID, &v.PipelineRID,
		&v.ManagedBy); err != nil {
		return nil, err
	}
	models.EnrichObjectTypeMetadata(v, nil)
	return v, nil
}

func (r *Repo) enrichObjectTypeMetadata(ctx context.Context, objectType *models.ObjectType) error {
	properties, err := r.ListProperties(ctx, objectType.ID)
	if err != nil {
		return err
	}
	models.EnrichObjectTypeMetadata(objectType, properties)
	return nil
}

// ── Properties ─────────────────────────────────────────────────────────

const propertyColumns = `id, object_type_id, name, display_name, description, property_type,
	required, unique_constraint, time_dependent,
	default_value, validation_rules, inline_edit_config,
	created_at, updated_at`

func (r *Repo) ListProperties(ctx context.Context, typeID uuid.UUID) ([]models.Property, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+propertyColumns+` FROM ontology_schema.properties WHERE object_type_id = $1 ORDER BY created_at`,
		typeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Property{}
	for rows.Next() {
		p, err := scanProperty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *Repo) CreateProperty(ctx context.Context, typeID uuid.UUID, body *models.CreatePropertyRequest) (*models.Property, error) {
	id := uuid.New()
	dn := body.DisplayName
	if dn == "" {
		dn = body.Name
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO ontology_schema.properties
		 (id, object_type_id, name, display_name, description, property_type,
		  required, unique_constraint, time_dependent,
		  default_value, validation_rules, inline_edit_config,
		  created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
		 RETURNING `+propertyColumns,
		id, typeID, body.Name, dn, body.Description, body.PropertyType,
		body.Required, body.UniqueConstraint, body.TimeDependent,
		body.DefaultValue, body.ValidationRules, body.InlineEditConfig,
		time.Now().UTC())
	return scanProperty(row)
}

func scanProperty(r rowLikeT) (*models.Property, error) {
	p := &models.Property{}
	if err := r.Scan(&p.ID, &p.ObjectTypeID, &p.Name, &p.DisplayName, &p.Description,
		&p.PropertyType, &p.Required, &p.UniqueConstraint, &p.TimeDependent,
		&p.DefaultValue, &p.ValidationRules, &p.InlineEditConfig,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	models.EnrichPropertyMetadata(p)
	return p, nil
}

// ── Object type groups ─────────────────────────────────────────────────

const objectTypeGroupColumns = `g.id, g.name, g.display_name, g.description, g.visibility, g.status,
	g.owner_id, g.created_at, g.updated_at,
	COALESCE((SELECT array_agg(ot.id ORDER BY ot.display_name) FROM ontology_schema.object_types ot WHERE g.name = ANY(ot.group_names)), '{}'::uuid[]) AS object_type_ids,
	(SELECT pr.project_id FROM ontology_schema.ontology_project_resources pr WHERE pr.resource_kind = 'object_type_group' AND pr.resource_id = g.id LIMIT 1) AS project_id`

func (r *Repo) ListObjectTypeGroups(ctx context.Context, search string, limit, offset int64) ([]models.ObjectTypeGroup, int64, error) {
	pattern := "%" + search + "%"
	var total int64
	if err := r.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ontology_schema.object_type_groups
		 WHERE name ILIKE $1 OR display_name ILIKE $1 OR description ILIKE $1`,
		pattern,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT `+objectTypeGroupColumns+`
		 FROM ontology_schema.object_type_groups g
		 WHERE g.name ILIKE $1 OR g.display_name ILIKE $1 OR g.description ILIKE $1
		 ORDER BY g.display_name ASC, g.created_at DESC
		 LIMIT $2 OFFSET $3`,
		pattern, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.ObjectTypeGroup{}
	for rows.Next() {
		group, err := scanObjectTypeGroup(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *group)
	}
	return out, total, rows.Err()
}

func (r *Repo) GetObjectTypeGroup(ctx context.Context, id uuid.UUID) (*models.ObjectTypeGroup, error) {
	row := r.Pool.QueryRow(ctx, `SELECT `+objectTypeGroupColumns+` FROM ontology_schema.object_type_groups g WHERE g.id = $1`, id)
	group, err := scanObjectTypeGroup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return group, err
}

func (r *Repo) CreateObjectTypeGroup(ctx context.Context, body *models.CreateObjectTypeGroupRequest, ownerID uuid.UUID) (*models.ObjectTypeGroup, error) {
	id := uuid.New()
	if body.ID != nil && *body.ID != uuid.Nil {
		id = *body.ID
	}
	displayName := body.DisplayName
	if displayName == "" {
		displayName = body.Name
	}
	visibility := body.Visibility
	if visibility == "" {
		visibility = "normal"
	}
	status := body.Status
	if status == "" {
		status = "active"
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO ontology_schema.object_type_groups
		 (id, name, display_name, description, visibility, status, owner_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
		id, body.Name, displayName, body.Description, visibility, status, ownerID, time.Now().UTC()); err != nil {
		return nil, err
	}
	if body.ProjectID != nil && *body.ProjectID != uuid.Nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ontology_schema.ontology_project_resources (project_id, resource_kind, resource_id, bound_by)
			 VALUES ($1, 'object_type_group', $2, $3)
			 ON CONFLICT (resource_kind, resource_id) DO UPDATE SET project_id = EXCLUDED.project_id, bound_by = EXCLUDED.bound_by`,
			*body.ProjectID, id, ownerID); err != nil {
			return nil, err
		}
	}
	if err := replaceObjectTypeGroupMembers(ctx, tx, body.Name, body.ObjectTypeIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetObjectTypeGroup(ctx, id)
}

func (r *Repo) UpdateObjectTypeGroup(ctx context.Context, id uuid.UUID, body *models.UpdateObjectTypeGroupRequest, actorID uuid.UUID) (*models.ObjectTypeGroup, error) {
	current, err := r.GetObjectTypeGroup(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	name := current.Name
	if body.Name != nil && *body.Name != "" {
		name = *body.Name
	}
	displayName := current.DisplayName
	if body.DisplayName != nil {
		displayName = *body.DisplayName
	}
	description := current.Description
	if body.Description != nil {
		description = *body.Description
	}
	visibility := current.Visibility
	if body.Visibility != nil && *body.Visibility != "" {
		visibility = *body.Visibility
	}
	status := current.Status
	if body.Status != nil && *body.Status != "" {
		status = *body.Status
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE ontology_schema.object_type_groups
		 SET name = $2, display_name = $3, description = $4, visibility = $5, status = $6, updated_at = $7
		 WHERE id = $1`,
		id, name, displayName, description, visibility, status, time.Now().UTC()); err != nil {
		return nil, err
	}
	if name != current.Name {
		if _, err := tx.Exec(ctx,
			`UPDATE ontology_schema.object_types
			 SET group_names = array_replace(group_names, $1, $2), updated_at = NOW()
			 WHERE $1 = ANY(group_names)`, current.Name, name); err != nil {
			return nil, err
		}
	}
	if body.ProjectID != nil {
		if *body.ProjectID == uuid.Nil {
			if _, err := tx.Exec(ctx, `DELETE FROM ontology_schema.ontology_project_resources WHERE resource_kind = 'object_type_group' AND resource_id = $1`, id); err != nil {
				return nil, err
			}
		} else if _, err := tx.Exec(ctx,
			`INSERT INTO ontology_schema.ontology_project_resources (project_id, resource_kind, resource_id, bound_by)
			 VALUES ($1, 'object_type_group', $2, $3)
			 ON CONFLICT (resource_kind, resource_id) DO UPDATE SET project_id = EXCLUDED.project_id, bound_by = EXCLUDED.bound_by`,
			*body.ProjectID, id, actorID); err != nil {
			return nil, err
		}
	}
	if body.ObjectTypeIDs != nil {
		if err := replaceObjectTypeGroupMembers(ctx, tx, name, *body.ObjectTypeIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetObjectTypeGroup(ctx, id)
}

func (r *Repo) DeleteObjectTypeGroup(ctx context.Context, id uuid.UUID) (bool, error) {
	current, err := r.GetObjectTypeGroup(ctx, id)
	if err != nil || current == nil {
		return false, err
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE ontology_schema.object_types SET group_names = array_remove(group_names, $1), updated_at = NOW() WHERE $1 = ANY(group_names)`, current.Name); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ontology_schema.ontology_project_resources WHERE resource_kind = 'object_type_group' AND resource_id = $1`, id); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM ontology_schema.object_type_groups WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) AddObjectTypeToGroup(ctx context.Context, groupID, objectTypeID uuid.UUID) (*models.ObjectTypeGroup, error) {
	group, err := r.GetObjectTypeGroup(ctx, groupID)
	if err != nil || group == nil {
		return group, err
	}
	_, err = r.Pool.Exec(ctx,
		`UPDATE ontology_schema.object_types
		 SET group_names = CASE WHEN $1 = ANY(group_names) THEN group_names ELSE array_append(group_names, $1) END,
		     updated_at = NOW()
		 WHERE id = $2`,
		group.Name, objectTypeID)
	if err != nil {
		return nil, err
	}
	return r.GetObjectTypeGroup(ctx, groupID)
}

func (r *Repo) RemoveObjectTypeFromGroup(ctx context.Context, groupID, objectTypeID uuid.UUID) (*models.ObjectTypeGroup, error) {
	group, err := r.GetObjectTypeGroup(ctx, groupID)
	if err != nil || group == nil {
		return group, err
	}
	_, err = r.Pool.Exec(ctx,
		`UPDATE ontology_schema.object_types
		 SET group_names = array_remove(group_names, $1), updated_at = NOW()
		 WHERE id = $2`,
		group.Name, objectTypeID)
	if err != nil {
		return nil, err
	}
	return r.GetObjectTypeGroup(ctx, groupID)
}

func replaceObjectTypeGroupMembers(ctx context.Context, tx pgx.Tx, groupName string, objectTypeIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE ontology_schema.object_types SET group_names = array_remove(group_names, $1), updated_at = NOW() WHERE $1 = ANY(group_names)`, groupName); err != nil {
		return err
	}
	for _, objectTypeID := range objectTypeIDs {
		if objectTypeID == uuid.Nil {
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE ontology_schema.object_types
			 SET group_names = CASE WHEN $1 = ANY(group_names) THEN group_names ELSE array_append(group_names, $1) END,
			     updated_at = NOW()
			 WHERE id = $2`, groupName, objectTypeID); err != nil {
			return err
		}
	}
	return nil
}

func scanObjectTypeGroup(r rowLikeT) (*models.ObjectTypeGroup, error) {
	group := &models.ObjectTypeGroup{}
	if err := r.Scan(&group.ID, &group.Name, &group.DisplayName, &group.Description,
		&group.Visibility, &group.Status, &group.OwnerID, &group.CreatedAt, &group.UpdatedAt,
		&group.ObjectTypeIDs, &group.ProjectID); err != nil {
		return nil, err
	}
	group.ObjectTypeCount = len(group.ObjectTypeIDs)
	return group, nil
}

// ── Link types ─────────────────────────────────────────────────────────

const linkTypeColumns = `id, name, display_name, description, source_type_id, target_type_id,
	cardinality, label, reverse_label, visibility, link_datasource_mapping, owner_id, created_at, updated_at`

func (r *Repo) ListLinkTypes(ctx context.Context, objectTypeID *uuid.UUID) ([]models.LinkType, error) {
	q := `SELECT ` + linkTypeColumns + ` FROM ontology_schema.link_types`
	args := []any{}
	if objectTypeID != nil {
		q += ` WHERE source_type_id = $1 OR target_type_id = $1`
		args = append(args, *objectTypeID)
	}
	q += ` ORDER BY created_at`
	rows, err := r.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkType{}
	for rows.Next() {
		lt, err := scanLinkType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *lt)
	}
	return out, rows.Err()
}

func (r *Repo) GetLinkType(ctx context.Context, id uuid.UUID) (*models.LinkType, error) {
	row := r.Pool.QueryRow(ctx, `SELECT `+linkTypeColumns+` FROM ontology_schema.link_types WHERE id = $1`, id)
	lt, err := scanLinkType(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return lt, err
}

func (r *Repo) CreateLinkType(ctx context.Context, body *models.CreateLinkTypeRequest, ownerID uuid.UUID) (*models.LinkType, error) {
	id := uuid.New()
	if body.ID != nil && *body.ID != uuid.Nil {
		id = *body.ID
	}
	dn := body.DisplayName
	if dn == "" {
		dn = body.Name
	}
	card := body.Cardinality
	if card == "" {
		card = "many_to_many"
	}
	visibility := body.Visibility
	if visibility == "" {
		visibility = "normal"
	}
	mapping := body.LinkDatasourceMapping
	if mapping == nil {
		mapping = map[string]any{}
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO ontology_schema.link_types
		 (id, name, display_name, description, source_type_id, target_type_id,
		  cardinality, label, reverse_label, visibility, link_datasource_mapping, owner_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
		 RETURNING `+linkTypeColumns,
		id, body.Name, dn, body.Description, body.SourceTypeID, body.TargetTypeID,
		card, body.Label, body.ReverseLabel, visibility, mapping, ownerID, time.Now().UTC())
	return scanLinkType(row)
}

func (r *Repo) UpdateLinkType(ctx context.Context, id uuid.UUID, body *models.UpdateLinkTypeRequest) (*models.LinkType, error) {
	current, err := r.GetLinkType(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	displayName := current.DisplayName
	if body.DisplayName != nil {
		displayName = *body.DisplayName
	}
	description := current.Description
	if body.Description != nil {
		description = *body.Description
	}
	cardinality := current.Cardinality
	if body.Cardinality != nil && *body.Cardinality != "" {
		cardinality = *body.Cardinality
	}
	label := current.Label
	if body.Label != nil {
		label = *body.Label
	}
	reverseLabel := current.ReverseLabel
	if body.ReverseLabel != nil {
		reverseLabel = *body.ReverseLabel
	}
	visibility := current.Visibility
	if body.Visibility != nil && *body.Visibility != "" {
		visibility = *body.Visibility
	}
	mapping := current.LinkDatasourceMapping
	if body.LinkDatasourceMapping != nil {
		mapping = body.LinkDatasourceMapping
	}
	if mapping == nil {
		mapping = map[string]any{}
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE ontology_schema.link_types SET
		   display_name = $2,
		   description = $3,
		   cardinality = $4,
		   label = $5,
		   reverse_label = $6,
		   visibility = $7,
		   link_datasource_mapping = $8,
		   updated_at = $9
		 WHERE id = $1
		 RETURNING `+linkTypeColumns,
		id, displayName, description, cardinality, label, reverseLabel, visibility, mapping, time.Now().UTC())
	return scanLinkType(row)
}

func (r *Repo) DeleteLinkType(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM ontology_schema.link_types WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func scanLinkType(r rowLikeT) (*models.LinkType, error) {
	lt := &models.LinkType{}
	if err := r.Scan(&lt.ID, &lt.Name, &lt.DisplayName, &lt.Description,
		&lt.SourceTypeID, &lt.TargetTypeID, &lt.Cardinality, &lt.Label, &lt.ReverseLabel,
		&lt.Visibility, &lt.LinkDatasourceMapping, &lt.OwnerID, &lt.CreatedAt, &lt.UpdatedAt); err != nil {
		return nil, err
	}
	return lt, nil
}

var _ = errors.New
var _ = pgx.ErrNoRows
