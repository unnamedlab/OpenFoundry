// Package interfaces ports
// `libs/ontology-kernel/src/handlers/interfaces.rs` 1:1: 11 endpoints
// covering ontology interface CRUD, interface property CRUD, and the
// (object_type ↔ interface) attachment surface.
//
// Project-access guards layer on top of the SQL via the helpers in
// [domain/project_access.go]; SQL strings are preserved byte-for-byte
// from the Rust source.

package interfaces

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

// Mount registers every endpoint on the chi router under the same
// path/verb shape as `lib.rs::build_router`.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Post("/ontology/interfaces", CreateInterface(state))
	r.Get("/ontology/interfaces", ListInterfaces(state))
	r.Get("/ontology/interfaces/{id}", GetInterface(state))
	r.Patch("/ontology/interfaces/{id}", UpdateInterface(state))
	r.Delete("/ontology/interfaces/{id}", DeleteInterface(state))

	r.Get("/ontology/interfaces/{id}/properties", ListInterfaceProperties(state))
	r.Post("/ontology/interfaces/{id}/properties", CreateInterfaceProperty(state))
	r.Patch("/ontology/interfaces/{id}/properties/{property_id}", UpdateInterfaceProperty(state))
	r.Delete("/ontology/interfaces/{id}/properties/{property_id}", DeleteInterfaceProperty(state))

	r.Post("/ontology/types/{type_id}/interfaces/{interface_id}", AttachInterface(state))
	r.Get("/ontology/types/{type_id}/interfaces", ListTypeInterfaces(state))
	r.Delete("/ontology/types/{type_id}/interfaces/{interface_id}", DetachInterface(state))
}

// ── HTTP plumbing ─────────────────────────────────────────────────────────

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
func forbidden(w http.ResponseWriter, m string)  { writeJSON(w, http.StatusForbidden, errBody(m)) }
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

// ── Access guards (mirror the four Rust helpers) ──────────────────────────

func ensureInterfaceViewAccess(r *http.Request, state *ontologykernel.AppState, claims *authmw.Claims, interfaceID uuid.UUID) error {
	pid, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindInterface, interfaceID)
	if err != nil {
		return errors.New("failed to load interface binding: " + err.Error())
	}
	return domain.EnsureResourceViewAccess(r.Context(), state.DB, claims, pid)
}

// ensureInterfaceManageAccess mirrors the Rust helper. Returns the
// verbatim "interface not found" string when the resource owner
// cannot be resolved (so the caller can map to 404 instead of 403).
func ensureInterfaceManageAccess(r *http.Request, state *ontologykernel.AppState, claims *authmw.Claims, interfaceID uuid.UUID) error {
	owner, err := domain.LoadResourceOwnerID(r.Context(), state.DB, domain.OntologyResourceKindInterface, interfaceID)
	if err != nil {
		return err
	}
	if owner == nil {
		return errors.New("interface not found")
	}
	pid, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindInterface, interfaceID)
	if err != nil {
		return errors.New("failed to load interface binding: " + err.Error())
	}
	return domain.EnsureResourceManageAccess(r.Context(), state.DB, claims, *owner, pid)
}

func ensureObjectTypeViewAccess(r *http.Request, state *ontologykernel.AppState, claims *authmw.Claims, objectTypeID uuid.UUID) error {
	pid, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindObjectType, objectTypeID)
	if err != nil {
		return errors.New("failed to load object type binding: " + err.Error())
	}
	return domain.EnsureResourceViewAccess(r.Context(), state.DB, claims, pid)
}

func ensureObjectTypeManageAccess(r *http.Request, state *ontologykernel.AppState, claims *authmw.Claims, objectTypeID uuid.UUID) error {
	owner, err := domain.LoadResourceOwnerID(r.Context(), state.DB, domain.OntologyResourceKindObjectType, objectTypeID)
	if err != nil {
		return err
	}
	if owner == nil {
		return errors.New("object type not found")
	}
	pid, err := domain.LoadResourceProjectID(r.Context(), state.DB, domain.OntologyResourceKindObjectType, objectTypeID)
	if err != nil {
		return errors.New("failed to load object type binding: " + err.Error())
	}
	return domain.EnsureResourceManageAccess(r.Context(), state.DB, claims, *owner, pid)
}

// ── Endpoints ─────────────────────────────────────────────────────────────

const interfaceColumns = `id, name, display_name, description, owner_id, created_at, updated_at`

const interfacePropertyColumns = `id, interface_id, name, display_name, description, property_type,
                  required, unique_constraint, time_dependent, default_value, validation_rules,
                  created_at, updated_at`

// CreateInterface mirrors `pub async fn create_interface`.
func CreateInterface(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		var body models.CreateInterfaceRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			badRequest(w, "interface name is required")
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
			`INSERT INTO ontology_interfaces (id, name, display_name, description, owner_id)
               VALUES ($1, $2, $3, $4, $5)
               RETURNING `+interfaceColumns,
			id, body.Name, displayName, description, claims.Sub,
		)
		out, err := scanInterface(row)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

// ListInterfaces mirrors `pub async fn list_interfaces`. The
// Rust source applies the visibility filter AFTER the LIMIT/OFFSET
// pull (it counts and slices the filtered subset, not the global
// row set) — Go reproduces the same shape so total + page work
// against the visible window.
func ListInterfaces(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		page, perPage, search := parsePagination(r)
		offset := (page - 1) * perPage
		searchPattern := "%" + search + "%"

		rows, err := state.DB.Query(r.Context(),
			`SELECT `+interfaceColumns+`
               FROM ontology_interfaces
               WHERE name ILIKE $1 OR display_name ILIKE $1
               ORDER BY created_at DESC
               LIMIT $2 OFFSET $3`,
			searchPattern, perPage, offset,
		)
		if err != nil {
			internalError(w, "list interfaces failed")
			return
		}
		defer rows.Close()
		data := []models.OntologyInterface{}
		for rows.Next() {
			rec, err := scanInterface(rows)
			if err == nil {
				data = append(data, rec)
			}
		}

		accessible, err := domain.ListAccessibleProjects(r.Context(), state.DB, claims)
		if err != nil {
			internalError(w, "list interfaces project access failed")
			return
		}
		ids := make([]uuid.UUID, 0, len(data))
		for _, d := range data {
			ids = append(ids, d.ID)
		}
		projectMap, err := domain.LoadResourceProjectMap(r.Context(), state.DB, domain.OntologyResourceKindInterface, ids)
		if err != nil {
			internalError(w, "list interfaces bindings failed")
			return
		}
		visible := make([]models.OntologyInterface, 0, len(data))
		for _, d := range data {
			var pid *uuid.UUID
			if v, ok := projectMap[d.ID]; ok {
				pid = &v
			}
			if domain.ResourceIsVisible(claims, pid, accessible) {
				visible = append(visible, d)
			}
		}
		// Rust then applies `take(per_page)` over the filtered slice
		// — already true here because we never grow past `perPage`
		// after the LIMIT in the SQL above.
		total := int64(len(visible))
		writeJSON(w, http.StatusOK, models.ListInterfacesResponse{
			Data:    visible,
			Total:   total,
			Page:    page,
			PerPage: perPage,
		})
	}
}

// GetInterface mirrors `pub async fn get_interface`.
func GetInterface(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+interfaceColumns+` FROM ontology_interfaces WHERE id = $1`,
			id,
		)
		intf, err := scanInterface(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "interface not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if err := ensureInterfaceViewAccess(r, state, claims, id); err != nil {
			forbidden(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, intf)
	}
}

// UpdateInterface mirrors `pub async fn update_interface`. The
// access-guard error string `"interface not found"` is mapped to
// 404 verbatim (rest map to 403). COALESCE keeps the prior values
// when PATCH body fields are nil.
func UpdateInterface(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if err := ensureInterfaceManageAccess(r, state, claims, id); err != nil {
			if err.Error() == "interface not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		var body models.UpdateInterfaceRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`UPDATE ontology_interfaces
               SET display_name = COALESCE($2, display_name),
                   description = COALESCE($3, description),
                   updated_at = NOW()
               WHERE id = $1
               RETURNING `+interfaceColumns,
			id, body.DisplayName, body.Description,
		)
		intf, err := scanInterface(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "interface not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, intf)
	}
}

// DeleteInterface mirrors `pub async fn delete_interface`.
func DeleteInterface(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if err := ensureInterfaceManageAccess(r, state, claims, id); err != nil {
			if err.Error() == "interface not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM ontology_interfaces WHERE id = $1`,
			id,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "interface not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Interface properties ──────────────────────────────────────────────────

// ListInterfaceProperties mirrors `pub async fn list_interface_properties`.
func ListInterfaceProperties(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if err := ensureInterfaceViewAccess(r, state, claims, id); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT `+interfacePropertyColumns+`
               FROM interface_properties
               WHERE interface_id = $1
               ORDER BY created_at ASC`,
			id,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		defer rows.Close()
		data := []models.InterfaceProperty{}
		for rows.Next() {
			p, err := scanInterfaceProperty(rows)
			if err == nil {
				data = append(data, p)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// CreateInterfaceProperty mirrors `pub async fn create_interface_property`.
// Validates name + property_type + (when present) default_value.
func CreateInterfaceProperty(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		interfaceID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if err := ensureInterfaceManageAccess(r, state, claims, interfaceID); err != nil {
			if err.Error() == "interface not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		var body models.CreateInterfacePropertyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			badRequest(w, "property name is required")
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
		displayName := body.Name
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
			`INSERT INTO interface_properties (
                   id, interface_id, name, display_name, description, property_type,
                   required, unique_constraint, time_dependent, default_value, validation_rules
               )
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
               RETURNING `+interfacePropertyColumns,
			id, interfaceID, body.Name, displayName, description, body.PropertyType,
			required, uniq, timeDep, body.DefaultValue, body.ValidationRules,
		)
		p, err := scanInterfaceProperty(row)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	}
}

// UpdateInterfaceProperty mirrors `pub async fn update_interface_property`.
// Looks up the existing property first, runs the manage-access guard
// against its `interface_id`, then re-validates `default_value` against
// the immutable `property_type`.
func UpdateInterfaceProperty(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		propertyID, err := pathUUID(r, "property_id")
		if err != nil {
			badRequest(w, "invalid property_id")
			return
		}
		var body models.UpdateInterfacePropertyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+interfacePropertyColumns+` FROM interface_properties WHERE id = $1`,
			propertyID,
		)
		existing, err := scanInterfaceProperty(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "interface property not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if err := ensureInterfaceManageAccess(r, state, claims, existing.InterfaceID); err != nil {
			if err.Error() == "interface not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
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
			`UPDATE interface_properties
               SET display_name = COALESCE($2, display_name),
                   description = COALESCE($3, description),
                   required = COALESCE($4, required),
                   unique_constraint = COALESCE($5, unique_constraint),
                   time_dependent = COALESCE($6, time_dependent),
                   default_value = $7,
                   validation_rules = $8,
                   updated_at = NOW()
               WHERE id = $1
               RETURNING `+interfacePropertyColumns,
			propertyID, body.DisplayName, body.Description,
			body.Required, body.UniqueConstraint, body.TimeDependent,
			nextDefault, nextValidation,
		)
		p, err := scanInterfaceProperty(updateRow)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "interface property not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

// DeleteInterfaceProperty mirrors `pub async fn delete_interface_property`.
// Pulls the interface_id off the property first to scope the
// manage-access guard, then issues the DELETE.
func DeleteInterfaceProperty(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		propertyID, err := pathUUID(r, "property_id")
		if err != nil {
			badRequest(w, "invalid property_id")
			return
		}
		var interfaceID uuid.UUID
		err = state.DB.QueryRow(r.Context(),
			`SELECT interface_id FROM interface_properties WHERE id = $1`,
			propertyID,
		).Scan(&interfaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "interface property not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if err := ensureInterfaceManageAccess(r, state, claims, interfaceID); err != nil {
			if err.Error() == "interface not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM interface_properties WHERE id = $1`,
			propertyID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "interface property not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── ObjectType ↔ Interface bindings ──────────────────────────────────────

// AttachInterface mirrors `pub async fn attach_interface_to_type`.
// Both type and interface manage-access guards run sequentially;
// "object type not found" / "interface not found" map to 404, other
// errors map to 403.
func AttachInterface(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		interfaceID, err := pathUUID(r, "interface_id")
		if err != nil {
			badRequest(w, "invalid interface_id")
			return
		}
		for _, accessErr := range []error{
			ensureObjectTypeManageAccess(r, state, claims, typeID),
			ensureInterfaceManageAccess(r, state, claims, interfaceID),
		} {
			if accessErr == nil {
				continue
			}
			msg := accessErr.Error()
			if msg == "object type not found" || msg == "interface not found" {
				notFound(w, msg)
				return
			}
			forbidden(w, msg)
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO object_type_interfaces (object_type_id, interface_id)
               VALUES ($1, $2)
               ON CONFLICT (object_type_id, interface_id) DO NOTHING
               RETURNING object_type_id, interface_id, created_at`,
			typeID, interfaceID,
		)
		var binding models.ObjectTypeInterfaceBinding
		err = row.Scan(&binding.ObjectTypeID, &binding.InterfaceID, &binding.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{
				"object_type_id": typeID,
				"interface_id":   interfaceID,
				"status":         "attached",
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

// ListTypeInterfaces mirrors `pub async fn list_type_interfaces`.
func ListTypeInterfaces(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		if err := ensureObjectTypeViewAccess(r, state, claims, typeID); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT i.id, i.name, i.display_name, i.description, i.owner_id, i.created_at, i.updated_at
               FROM ontology_interfaces i
               INNER JOIN object_type_interfaces oti ON oti.interface_id = i.id
               WHERE oti.object_type_id = $1
               ORDER BY i.created_at ASC`,
			typeID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		defer rows.Close()
		data := []models.OntologyInterface{}
		for rows.Next() {
			rec, err := scanInterface(rows)
			if err == nil {
				data = append(data, rec)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// DetachInterface mirrors `pub async fn detach_interface_from_type`.
func DetachInterface(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		interfaceID, err := pathUUID(r, "interface_id")
		if err != nil {
			badRequest(w, "invalid interface_id")
			return
		}
		for _, accessErr := range []error{
			ensureObjectTypeManageAccess(r, state, claims, typeID),
			ensureInterfaceManageAccess(r, state, claims, interfaceID),
		} {
			if accessErr == nil {
				continue
			}
			msg := accessErr.Error()
			if msg == "object type not found" || msg == "interface not found" {
				notFound(w, msg)
				return
			}
			forbidden(w, msg)
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM object_type_interfaces WHERE object_type_id = $1 AND interface_id = $2`,
			typeID, interfaceID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "interface binding not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func parsePagination(r *http.Request) (page int64, perPage int64, search string) {
	page = int64(1)
	perPage = int64(20)
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
		search = raw
	}
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage, search
}

func scanInterface(row interface{ Scan(...any) error }) (models.OntologyInterface, error) {
	var i models.OntologyInterface
	err := row.Scan(
		&i.ID, &i.Name, &i.DisplayName, &i.Description,
		&i.OwnerID, &i.CreatedAt, &i.UpdatedAt,
	)
	return i, err
}

func scanInterfaceProperty(row interface{ Scan(...any) error }) (models.InterfaceProperty, error) {
	var p models.InterfaceProperty
	err := row.Scan(
		&p.ID, &p.InterfaceID, &p.Name, &p.DisplayName, &p.Description, &p.PropertyType,
		&p.Required, &p.UniqueConstraint, &p.TimeDependent,
		&p.DefaultValue, &p.ValidationRules,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}
