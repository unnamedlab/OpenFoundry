// Package types ports `libs/ontology-kernel/src/handlers/types.rs`
// 1:1: 5 endpoints for ObjectType CRUD under
// `/api/v1/ontology/types`.
//
// Project access checks layer on top of the SQL: every read goes
// through [domain.EnsureResourceViewAccess] / list filters via
// [domain.ResourceIsVisible]; every write goes through
// [domain.EnsureResourceManageAccess]. The SQL is preserved
// byte-for-byte so the same migrations + indexes back both ports
// during the migration window.
//
// Endpoints (matched against `lib.rs::build_router::types_routes`):
//
//   - POST   /ontology/types        → CreateObjectType
//   - GET    /ontology/types        → ListObjectTypes
//   - GET    /ontology/types/{id}   → GetObjectType
//   - PATCH  /ontology/types/{id}   → UpdateObjectType
//   - DELETE /ontology/types/{id}   → DeleteObjectType

package types

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// Mount registers every ObjectType endpoint on the chi router under
// the same path / verb shape as `lib.rs::build_router`.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Post("/ontology/types", CreateObjectType(state))
	r.Get("/ontology/types", ListObjectTypes(state))
	r.Get("/ontology/types/{id}", GetObjectType(state))
	r.Patch("/ontology/types/{id}", UpdateObjectType(state))
	r.Delete("/ontology/types/{id}", DeleteObjectType(state))
}

// ── HTTP plumbing ────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func unauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
}
func forbidden(w http.ResponseWriter, m string) { writeJSON(w, http.StatusForbidden, errBody(m)) }
func internalError(w http.ResponseWriter, m string) {
	writeJSON(w, http.StatusInternalServerError, errBody(m))
}

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errors.New("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

// ── Endpoints ────────────────────────────────────────────────────────────

const objectTypeColumns = `id, name, display_name, plural_display_name, description, primary_key_property, title_property, icon, color, status, visibility, group_names, object_display_preferences, owner_id, created_at, updated_at`

// CreateObjectType mirrors `pub async fn create_object_type`. Body
// fields default verbatim with the Rust source: empty
// `display_name` falls back to `name`; missing `description`
// defaults to "".
func CreateObjectType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		var body models.CreateObjectTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid request body"))
			return
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		displayName := body.Name
		if body.DisplayName != nil {
			displayName = *body.DisplayName
		}
		description := ""
		if body.Description != nil {
			description = *body.Description
		}

		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO object_types (id, name, display_name, plural_display_name, description, primary_key_property, title_property, icon, color, status, visibility, group_names, object_display_preferences, owner_id)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10, 'active'), COALESCE($11, 'normal'), COALESCE($12, '{}'), COALESCE($13, '{}'::jsonb), $14)
               RETURNING `+objectTypeColumns+``,
			id, body.Name, displayName, body.PluralDisplayName, description,
			body.PrimaryKeyProperty, body.TitleProperty, body.Icon, body.Color, body.Status, body.Visibility, body.GroupNames, body.ObjectDisplayPreferences,
			claims.Sub,
		)
		ot, err := scanObjectType(row)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, ot)
	}
}

// ListObjectTypes mirrors `pub async fn list_object_types`. The
// project-visibility filter is applied in-memory after the SQL pull,
// matching the Rust source's pattern (so the total / pagination
// reflect what the caller can actually see).
func ListObjectTypes(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}

		page := int64(1)
		perPage := int64(20)
		searchValue := ""
		if raw := r.URL.Query().Get("page"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 1 {
				page = v
			}
		}
		if raw := r.URL.Query().Get("per_page"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
				perPage = v
			}
		}
		if raw := r.URL.Query().Get("search"); raw != "" {
			searchValue = raw
		}
		if perPage < 1 {
			perPage = 1
		}
		if perPage > 100 {
			perPage = 100
		}
		offset := (page - 1) * perPage
		searchPattern := "%" + searchValue + "%"

		rows, err := state.DB.Query(r.Context(),
			`SELECT `+objectTypeColumns+`
               FROM object_types
               WHERE name ILIKE $1 OR display_name ILIKE $1
               ORDER BY created_at DESC`,
			searchPattern,
		)
		var allTypes []models.ObjectType
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				ot, err := scanObjectType(rows)
				if err == nil {
					allTypes = append(allTypes, ot)
				}
			}
		}

		accessible, err := domain.ListAccessibleProjects(r.Context(), state.DB, claims)
		if err != nil {
			internalError(w, "list object types project access: "+err.Error())
			return
		}
		ids := make([]uuid.UUID, 0, len(allTypes))
		for _, t := range allTypes {
			ids = append(ids, t.ID)
		}
		projectMap, err := domain.LoadResourceProjectMap(r.Context(), state.DB, domain.OntologyResourceKindObjectType, ids)
		if err != nil {
			internalError(w, "list object types project bindings: "+err.Error())
			return
		}

		visible := make([]models.ObjectType, 0, len(allTypes))
		for _, t := range allTypes {
			var pid *uuid.UUID
			if v, ok := projectMap[t.ID]; ok {
				pid = &v
			}
			if domain.ResourceIsVisible(claims, pid, accessible) {
				visible = append(visible, t)
			}
		}
		total := int64(len(visible))

		// Apply offset + limit AFTER the visibility filter so the
		// totals reflect what the caller can actually see (mirrors
		// Rust's `skip(offset).take(per_page)` order).
		if offset >= total {
			visible = []models.ObjectType{}
		} else {
			end := offset + perPage
			if end > total {
				end = total
			}
			visible = visible[offset:end]
		}
		for i := range visible {
			properties, err := loadObjectTypeProperties(r.Context(), state, visible[i].ID)
			if err != nil {
				internalError(w, "list object type properties: "+err.Error())
				return
			}
			models.EnrichObjectTypeMetadata(&visible[i], properties)
		}
		writeJSON(w, http.StatusOK, models.ListObjectTypesResponse{
			Data:    visible,
			Total:   total,
			Page:    page,
			PerPage: perPage,
		})
	}
}

// GetObjectType mirrors `pub async fn get_object_type`. The view-
// access guard runs after the SELECT so a 404 is surfaced before
// the 403, matching the Rust source.
func GetObjectType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid path id"))
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+objectTypeColumns+`
               FROM object_types WHERE id = $1`,
			id,
		)
		ot, err := scanObjectType(row)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody("object type not found"))
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		projectID, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindObjectType, id)
		if err != nil {
			internalError(w, "get object type project binding: "+err.Error())
			return
		}
		if err := domain.EnsureResourceViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		properties, err := loadObjectTypeProperties(r.Context(), state, ot.ID)
		if err != nil {
			internalError(w, "get object type properties: "+err.Error())
			return
		}
		models.EnrichObjectTypeMetadata(&ot, properties)
		writeJSON(w, http.StatusOK, ot)
	}
}

// UpdateObjectType mirrors `pub async fn update_object_type`. The
// COALESCE-based UPDATE preserves the existing column when the
// PATCH body leaves a field nil — same behaviour as the Rust
// source's `bind(&body.field)`.
func UpdateObjectType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid path id"))
			return
		}
		var body models.UpdateObjectTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid request body"))
			return
		}

		row := state.DB.QueryRow(r.Context(),
			`SELECT `+objectTypeColumns+`
               FROM object_types WHERE id = $1`,
			id,
		)
		existing, err := scanObjectType(row)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody("object type not found"))
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		projectID, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindObjectType, id)
		if err != nil {
			internalError(w, "update object type project binding: "+err.Error())
			return
		}
		if err := domain.EnsureResourceManageAccess(r.Context(), state.DB, claims, existing.OwnerID, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}

		updateRow := state.DB.QueryRow(r.Context(),
			`UPDATE object_types SET
                   name = COALESCE($2, name),
                   display_name = COALESCE($3, display_name),
                   plural_display_name = COALESCE($4, plural_display_name),
                   description = COALESCE($5, description),
                   primary_key_property = COALESCE($6, primary_key_property),
                   title_property = COALESCE($7, title_property),
                   icon = COALESCE($8, icon),
                   color = COALESCE($9, color),
                   status = COALESCE($10, status),
                   visibility = COALESCE($11, visibility),
                   group_names = COALESCE($12, group_names),
                   object_display_preferences = COALESCE($13, object_display_preferences),
                   updated_at = NOW()
               WHERE id = $1
               RETURNING `+objectTypeColumns+``,
			id, body.Name, body.DisplayName, body.PluralDisplayName, body.Description, body.PrimaryKeyProperty, body.TitleProperty, body.Icon, body.Color, body.Status, body.Visibility, body.GroupNames, body.ObjectDisplayPreferences,
		)
		ot, err := scanObjectType(updateRow)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody("object type not found"))
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ot)
	}
}

// DeleteObjectType mirrors `pub async fn delete_object_type`.
// Manage-access guard precedes the DELETE; missing rows surface
// 404 verbatim with the Rust dispatch.
func DeleteObjectType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid path id"))
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+objectTypeColumns+`
               FROM object_types WHERE id = $1`,
			id,
		)
		existing, err := scanObjectType(row)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody("object type not found"))
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		projectID, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindObjectType, id)
		if err != nil {
			internalError(w, "delete object type project binding: "+err.Error())
			return
		}
		if err := domain.EnsureResourceManageAccess(r.Context(), state.DB, claims, existing.OwnerID, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(), `DELETE FROM object_types WHERE id = $1`, id)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, errBody("object type not found"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// scanObjectType reads a row off pgx.Row or pgx.Rows.
func scanObjectType(row interface{ Scan(...any) error }) (models.ObjectType, error) {
	var t models.ObjectType
	err := row.Scan(
		&t.ID, &t.Name, &t.DisplayName, &t.PluralDisplayName, &t.Description,
		&t.PrimaryKeyProperty, &t.TitleProperty, &t.Icon, &t.Color, &t.Status, &t.Visibility, &t.GroupNames, &t.ObjectDisplayPreferences,
		&t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
	)
	models.EnrichObjectTypeMetadata(&t, nil)
	return t, err
}

const objectTypePropertyColumns = `id, object_type_id, name, display_name, description, property_type, required,
                  unique_constraint, time_dependent, default_value, validation_rules,
                  inline_edit_config, created_at, updated_at`

func loadObjectTypeProperties(ctx context.Context, state *ontologykernel.AppState, objectTypeID uuid.UUID) ([]models.Property, error) {
	rows, err := state.DB.Query(ctx,
		`SELECT `+objectTypePropertyColumns+`
           FROM properties
           WHERE object_type_id = $1
           ORDER BY created_at ASC`,
		objectTypeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	properties := []models.Property{}
	for rows.Next() {
		property, err := scanObjectTypeProperty(rows)
		if err != nil {
			return nil, err
		}
		properties = append(properties, property)
	}
	return properties, rows.Err()
}

func scanObjectTypeProperty(row interface{ Scan(...any) error }) (models.Property, error) {
	var (
		property  models.Property
		inlineRaw []byte
	)
	err := row.Scan(
		&property.ID, &property.ObjectTypeID, &property.Name, &property.DisplayName, &property.Description,
		&property.PropertyType, &property.Required, &property.UniqueConstraint, &property.TimeDependent,
		&property.DefaultValue, &property.ValidationRules, &inlineRaw,
		&property.CreatedAt, &property.UpdatedAt,
	)
	if err != nil {
		return property, err
	}
	if len(inlineRaw) > 0 && string(inlineRaw) != "null" {
		var cfg models.PropertyInlineEditConfig
		if err := json.Unmarshal(inlineRaw, &cfg); err == nil {
			property.InlineEditConfig = &cfg
		}
	}
	models.EnrichPropertyMetadata(&property)
	return property, nil
}
