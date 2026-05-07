// Package sharedproperties ports
// `libs/ontology-kernel/src/handlers/shared_properties.rs` 1:1: 7
// endpoints for shared property type CRUD + the (object_type ↔
// shared_property_type) attachment surface under
// `/api/v1/ontology/shared-property-types`.
//
// The Rust source lives at module path `handlers::shared_properties`;
// the Go package name omits the underscore (`sharedproperties`)
// because Go style favours single-word package identifiers — the
// import path keeps the hyphen so chi mounts retain the same shape.
//
// Endpoints (matched against `lib.rs::build_router`):
//
//   - GET    /ontology/shared-property-types                                   → ListSharedPropertyTypes
//   - POST   /ontology/shared-property-types                                   → CreateSharedPropertyType
//   - GET    /ontology/shared-property-types/{id}                              → GetSharedPropertyType
//   - PATCH  /ontology/shared-property-types/{id}                              → UpdateSharedPropertyType
//   - DELETE /ontology/shared-property-types/{id}                              → DeleteSharedPropertyType
//   - POST   /ontology/types/{type_id}/shared-property-types/{shared_id}       → AttachSharedPropertyType
//   - GET    /ontology/types/{type_id}/shared-property-types                   → ListTypeSharedPropertyTypes
//   - DELETE /ontology/types/{type_id}/shared-property-types/{shared_id}       → DetachSharedPropertyType

package sharedproperties

import (
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

// Mount registers every shared-property-types endpoint on the chi
// router under the same path / verb shape as `lib.rs::build_router`.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Get("/ontology/shared-property-types", ListSharedPropertyTypes(state))
	r.Post("/ontology/shared-property-types", CreateSharedPropertyType(state))
	r.Get("/ontology/shared-property-types/{id}", GetSharedPropertyType(state))
	r.Patch("/ontology/shared-property-types/{id}", UpdateSharedPropertyType(state))
	r.Delete("/ontology/shared-property-types/{id}", DeleteSharedPropertyType(state))
	r.Post("/ontology/types/{type_id}/shared-property-types/{shared_id}", AttachSharedPropertyType(state))
	r.Get("/ontology/types/{type_id}/shared-property-types", ListTypeSharedPropertyTypes(state))
	r.Delete("/ontology/types/{type_id}/shared-property-types/{shared_id}", DetachSharedPropertyType(state))
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

func unauthorized(w http.ResponseWriter)        { writeJSON(w, http.StatusUnauthorized, errBody("missing claims")) }
func badRequest(w http.ResponseWriter, m string) { writeJSON(w, http.StatusBadRequest, errBody(m)) }
func notFound(w http.ResponseWriter, m string)   { writeJSON(w, http.StatusNotFound, errBody(m)) }
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

const sharedPropertyTypeColumns = `id, name, display_name, description, property_type, required, unique_constraint,
                  time_dependent, default_value, validation_rules, owner_id, created_at, updated_at`

// ListSharedPropertyTypes mirrors `pub async fn list_shared_property_types`.
func ListSharedPropertyTypes(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
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

		var total int64
		err := state.DB.QueryRow(r.Context(),
			`SELECT COUNT(*)
               FROM shared_property_types
               WHERE name ILIKE $1 OR display_name ILIKE $1 OR property_type ILIKE $1`,
			searchPattern,
		).Scan(&total)
		if err != nil {
			// Rust unwrap_or(0) → tolerate count failures silently.
			total = 0
		}

		rows, err := state.DB.Query(r.Context(),
			`SELECT `+sharedPropertyTypeColumns+`
               FROM shared_property_types
               WHERE name ILIKE $1 OR display_name ILIKE $1 OR property_type ILIKE $1
               ORDER BY created_at DESC
               LIMIT $2 OFFSET $3`,
			searchPattern, perPage, offset,
		)
		data := []models.SharedPropertyType{}
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				rec, err := scanSharedPropertyType(rows)
				if err == nil {
					data = append(data, rec)
				}
			}
		}
		writeJSON(w, http.StatusOK, models.ListSharedPropertyTypesResponse{
			Data:    data,
			Total:   total,
			Page:    page,
			PerPage: perPage,
		})
	}
}

// CreateSharedPropertyType mirrors `pub async fn create_shared_property_type`.
// Validates name + property_type + (when present) default_value
// before issuing the INSERT.
func CreateSharedPropertyType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		var body models.CreateSharedPropertyTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			badRequest(w, "shared property type name is required")
			return
		}
		if err := domain.ValidatePropertyType(body.PropertyType); err != nil {
			badRequest(w, err.Error())
			return
		}
		if len(body.DefaultValue) > 0 {
			if err := domain.ValidatePropertyValue(body.PropertyType, body.DefaultValue); err != nil {
				badRequest(w, err.Error())
				return
			}
		}

		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		name := strings.TrimSpace(body.Name)
		displayName := name
		if body.DisplayName != nil {
			displayName = *body.DisplayName
		}
		description := ""
		if body.Description != nil {
			description = *body.Description
		}
		required := false
		if body.Required != nil {
			required = *body.Required
		}
		uniq := false
		if body.UniqueConstraint != nil {
			uniq = *body.UniqueConstraint
		}
		timeDep := false
		if body.TimeDependent != nil {
			timeDep = *body.TimeDependent
		}

		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO shared_property_types (
                   id, name, display_name, description, property_type, required,
                   unique_constraint, time_dependent, default_value, validation_rules, owner_id
               )
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
               RETURNING `+sharedPropertyTypeColumns,
			id, name, displayName, description, body.PropertyType,
			required, uniq, timeDep,
			body.DefaultValue, body.ValidationRules,
			claims.Sub,
		)
		rec, err := scanSharedPropertyType(row)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, rec)
	}
}

// GetSharedPropertyType mirrors `pub async fn get_shared_property_type`.
func GetSharedPropertyType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+sharedPropertyTypeColumns+`
               FROM shared_property_types
               WHERE id = $1`,
			id,
		)
		rec, err := scanSharedPropertyType(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "shared property type not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// UpdateSharedPropertyType mirrors `pub async fn update_shared_property_type`.
// `default_value` and `validation_rules` overlay onto the existing
// row when nil — matches the Rust `body.default_value.or(existing.default_value)`
// pattern. The PROPERTY_TYPE itself is immutable post-create, so
// `validate_property_value` is run against `existing.property_type`.
func UpdateSharedPropertyType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		var body models.UpdateSharedPropertyTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+sharedPropertyTypeColumns+`
               FROM shared_property_types
               WHERE id = $1`,
			id,
		)
		existing, err := scanSharedPropertyType(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "shared property type not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}

		// `body.default_value.or(existing.default_value.clone())`
		nextDefault := body.DefaultValue
		if len(nextDefault) == 0 {
			nextDefault = existing.DefaultValue
		}
		if len(nextDefault) > 0 {
			if err := domain.ValidatePropertyValue(existing.PropertyType, nextDefault); err != nil {
				badRequest(w, err.Error())
				return
			}
		}
		nextValidation := body.ValidationRules
		if len(nextValidation) == 0 {
			nextValidation = existing.ValidationRules
		}

		updateRow := state.DB.QueryRow(r.Context(),
			`UPDATE shared_property_types
               SET display_name = COALESCE($2, display_name),
                   description = COALESCE($3, description),
                   required = COALESCE($4, required),
                   unique_constraint = COALESCE($5, unique_constraint),
                   time_dependent = COALESCE($6, time_dependent),
                   default_value = $7,
                   validation_rules = $8,
                   updated_at = NOW()
               WHERE id = $1
               RETURNING `+sharedPropertyTypeColumns,
			id,
			body.DisplayName, body.Description,
			body.Required, body.UniqueConstraint, body.TimeDependent,
			nextDefault, nextValidation,
		)
		rec, err := scanSharedPropertyType(updateRow)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "shared property type not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// DeleteSharedPropertyType mirrors `pub async fn delete_shared_property_type`.
func DeleteSharedPropertyType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM shared_property_types WHERE id = $1`,
			id,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "shared property type not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// AttachSharedPropertyType mirrors
// `pub async fn attach_shared_property_type_to_type`. The
// `ON CONFLICT … DO NOTHING` path returns 200 with a status payload
// (idempotent re-attach), the fresh-insert path returns 201 with the
// binding row.
func AttachSharedPropertyType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		sharedID, err := pathUUID(r, "shared_id")
		if err != nil {
			badRequest(w, "invalid shared_id")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO object_type_shared_property_types (object_type_id, shared_property_type_id)
               VALUES ($1, $2)
               ON CONFLICT (object_type_id, shared_property_type_id) DO NOTHING
               RETURNING object_type_id, shared_property_type_id, created_at`,
			typeID, sharedID,
		)
		var binding models.ObjectTypeSharedPropertyBinding
		err = row.Scan(&binding.ObjectTypeID, &binding.SharedPropertyTypeID, &binding.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			// Already attached — Rust returns the canned status payload.
			writeJSON(w, http.StatusOK, map[string]any{
				"object_type_id":          typeID,
				"shared_property_type_id": sharedID,
				"status":                  "attached",
			})
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, binding)
	}
}

// ListTypeSharedPropertyTypes mirrors
// `pub async fn list_type_shared_property_types`. SQL is
// byte-identical to the Rust source.
func ListTypeSharedPropertyTypes(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT spt.id, spt.name, spt.display_name, spt.description, spt.property_type,
                      spt.required, spt.unique_constraint, spt.time_dependent, spt.default_value,
                      spt.validation_rules, spt.owner_id, spt.created_at, spt.updated_at
               FROM shared_property_types spt
               INNER JOIN object_type_shared_property_types otsp
                    ON otsp.shared_property_type_id = spt.id
               WHERE otsp.object_type_id = $1
               ORDER BY otsp.created_at ASC, spt.created_at ASC`,
			typeID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		defer rows.Close()
		data := []models.SharedPropertyType{}
		for rows.Next() {
			rec, err := scanSharedPropertyType(rows)
			if err == nil {
				data = append(data, rec)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// DetachSharedPropertyType mirrors
// `pub async fn detach_shared_property_type_from_type`. Returns 204
// on success, 404 when the (object_type, shared_property_type) pair
// is unknown.
func DetachSharedPropertyType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authmw.FromContext(r.Context()); !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		sharedID, err := pathUUID(r, "shared_id")
		if err != nil {
			badRequest(w, "invalid shared_id")
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM object_type_shared_property_types
               WHERE object_type_id = $1 AND shared_property_type_id = $2`,
			typeID, sharedID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "shared property type binding not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// scanSharedPropertyType reads a row off pgx.Row or pgx.Rows.
func scanSharedPropertyType(row interface{ Scan(...any) error }) (models.SharedPropertyType, error) {
	var rec models.SharedPropertyType
	err := row.Scan(
		&rec.ID, &rec.Name, &rec.DisplayName, &rec.Description, &rec.PropertyType,
		&rec.Required, &rec.UniqueConstraint, &rec.TimeDependent,
		&rec.DefaultValue, &rec.ValidationRules,
		&rec.OwnerID, &rec.CreatedAt, &rec.UpdatedAt,
	)
	return rec, err
}
