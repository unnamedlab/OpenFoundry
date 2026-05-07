// Package properties ports
// `libs/ontology-kernel/src/handlers/properties.rs` 1:1: 4 endpoints
// for ObjectType property CRUD plus the inline-edit-action validator
// (TASK L) that gates the `inline_edit_config` payload against the
// referenced action type.
//
// Endpoints (matched against `lib.rs::build_router::properties_routes`):
//
//   - GET    /ontology/types/{type_id}/properties
//   - POST   /ontology/types/{type_id}/properties
//   - PATCH  /ontology/types/{type_id}/properties/{property_id}
//   - DELETE /ontology/types/{type_id}/properties/{property_id}

package properties

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// Mount registers every property endpoint on the chi router.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Get("/ontology/types/{type_id}/properties", ListProperties(state))
	r.Post("/ontology/types/{type_id}/properties", CreateProperty(state))
	r.Patch("/ontology/types/{type_id}/properties/{property_id}", UpdateProperty(state))
	r.Delete("/ontology/types/{type_id}/properties/{property_id}", DeleteProperty(state))
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

// ── Access guards ────────────────────────────────────────────────────────

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

// ── Endpoints ────────────────────────────────────────────────────────────

const propertyColumns = `id, object_type_id, name, display_name, description, property_type, required,
                  unique_constraint, time_dependent, default_value, validation_rules,
                  inline_edit_config, created_at, updated_at`

// ListProperties mirrors `pub async fn list_properties`.
func ListProperties(state *ontologykernel.AppState) http.HandlerFunc {
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
			`SELECT `+propertyColumns+`
               FROM properties
               WHERE object_type_id = $1
               ORDER BY created_at ASC`,
			typeID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		defer rows.Close()
		data := []models.Property{}
		for rows.Next() {
			p, err := scanProperty(rows)
			if err == nil {
				data = append(data, p)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// CreateProperty mirrors `pub async fn create_property`. The
// inline-edit-config gate (TASK L) lives in
// [validateInlineEditConfig] and validates that the referenced
// action type exists, belongs to the same object type, uses
// `update_object`, maps the property cleanly via input field, and
// has no side-effect notifications/webhooks.
func CreateProperty(state *ontologykernel.AppState) http.HandlerFunc {
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
		if err := ensureObjectTypeManageAccess(r, state, claims, typeID); err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		var body models.CreatePropertyRequest
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
		if body.InlineEditConfig != nil {
			if err := validateInlineEditConfig(r.Context(), state, typeID, body.Name, body.PropertyType, body.InlineEditConfig); err != nil {
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
		var inlineEditRaw json.RawMessage
		if body.InlineEditConfig != nil {
			inlineEditRaw, _ = json.Marshal(body.InlineEditConfig)
		}

		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO properties (
                   id, object_type_id, name, display_name, description, property_type,
                   required, unique_constraint, time_dependent, default_value, validation_rules,
                   inline_edit_config
               )
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
               RETURNING `+propertyColumns,
			id, typeID, body.Name, displayName, description, body.PropertyType,
			required, uniq, timeDep,
			body.DefaultValue, body.ValidationRules,
			inlineEditRaw,
		)
		p, err := scanProperty(row)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	}
}

// UpdateProperty mirrors `pub async fn update_property`.
//
// The `Option<Option<PropertyInlineEditConfig>>` semantics from the
// Rust source map onto the Go [models.PropertyInlineEditConfigUpdate]
// carrier:
//
//   - body.InlineEditConfig == nil      → no change (keep existing)
//   - body.InlineEditConfig.Value == nil → clear (set column to null)
//   - body.InlineEditConfig.Value != nil → replace
//
// Re-validation runs against the property's existing immutable
// `property_type`.
func UpdateProperty(state *ontologykernel.AppState) http.HandlerFunc {
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
		var body models.UpdatePropertyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT `+propertyColumns+` FROM properties WHERE id = $1`,
			propertyID,
		)
		existing, err := scanProperty(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "property not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if err := ensureObjectTypeManageAccess(r, state, claims, existing.ObjectTypeID); err != nil {
			if err.Error() == "object type not found" {
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

		// Three-way inline_edit_config: nil ⇒ keep existing, Set+Value=nil ⇒ clear, Set+Value≠nil ⇒ replace.
		nextInlineEdit := existing.InlineEditConfig
		clearInlineEdit := false
		if body.InlineEditConfig != nil {
			nextInlineEdit = body.InlineEditConfig.Value
			if nextInlineEdit == nil {
				clearInlineEdit = true
			}
		}
		if nextInlineEdit != nil {
			if err := validateInlineEditConfig(r.Context(), state, existing.ObjectTypeID, existing.Name, existing.PropertyType, nextInlineEdit); err != nil {
				badRequest(w, err.Error())
				return
			}
		}
		var inlineEditRaw json.RawMessage
		switch {
		case clearInlineEdit:
			inlineEditRaw = nil
		case nextInlineEdit != nil:
			inlineEditRaw, _ = json.Marshal(nextInlineEdit)
		}

		updateRow := state.DB.QueryRow(r.Context(),
			`UPDATE properties
               SET display_name = COALESCE($2, display_name),
                   description = COALESCE($3, description),
                   required = COALESCE($4, required),
                   unique_constraint = COALESCE($5, unique_constraint),
                   time_dependent = COALESCE($6, time_dependent),
                   default_value = $7,
                   validation_rules = $8,
                   inline_edit_config = $9,
                   updated_at = NOW()
               WHERE id = $1
               RETURNING `+propertyColumns,
			propertyID, body.DisplayName, body.Description,
			body.Required, body.UniqueConstraint, body.TimeDependent,
			nextDefault, nextValidation, inlineEditRaw,
		)
		p, err := scanProperty(updateRow)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "property not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

// DeleteProperty mirrors `pub async fn delete_property`. Pulls the
// property's object_type_id first to scope the manage-access guard.
func DeleteProperty(state *ontologykernel.AppState) http.HandlerFunc {
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
		var typeID uuid.UUID
		err = state.DB.QueryRow(r.Context(),
			`SELECT object_type_id FROM properties WHERE id = $1`,
			propertyID,
		).Scan(&typeID)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "property not found")
			return
		}
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if err := ensureObjectTypeManageAccess(r, state, claims, typeID); err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM properties WHERE id = $1`,
			propertyID,
		)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "property not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Inline-edit validator (TASK L) ───────────────────────────────────────

// extractOperationConfig mirrors `fn extract_operation_config`. When
// the action's config is the new "envelope" shape (carries
// `operation` or `notification_side_effects`), unwrap to the
// `operation` sub-object; otherwise return the config as-is. Rust
// source unwraps via the JSON object accessor.
func extractOperationConfig(config json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(config, &obj); err != nil || obj == nil {
		return config
	}
	_, hasOp := obj["operation"]
	_, hasNotif := obj["notification_side_effects"]
	if !hasOp && !hasNotif {
		return config
	}
	if v, ok := obj["operation"]; ok {
		return v
	}
	return json.RawMessage("null")
}

// resolveInlineEditInputName mirrors `fn resolve_inline_edit_input_name`.
// The valid mappings are the action's `update_object` config
// `property_mappings` entries that target the property by name. The
// return value is the input field name those mappings consume.
func resolveInlineEditInputName(action models.ActionType, propertyName string, cfg *models.PropertyInlineEditConfig) (string, error) {
	operationConfig := extractOperationConfig(action.Config)
	var update models.UpdateObjectActionConfig
	if err := json.Unmarshal(operationConfig, &update); err != nil {
		return "", errors.New("invalid inline edit action config: " + err.Error())
	}

	candidates := []string{}
	for _, mapping := range update.PropertyMappings {
		if mapping.PropertyName != propertyName {
			continue
		}
		if mapping.InputName != nil {
			candidates = append(candidates, *mapping.InputName)
		}
	}

	if cfg.InputName != nil {
		want := *cfg.InputName
		for _, candidate := range candidates {
			if candidate == want {
				return want, nil
			}
		}
		return "", errors.New("inline edit action does not map property '" + propertyName + "' from input '" + want + "'")
	}

	uniqueSet := map[string]bool{}
	for _, candidate := range candidates {
		uniqueSet[candidate] = true
	}
	switch len(uniqueSet) {
	case 0:
		return "", errors.New("inline edit action must map property '" + propertyName + "' from an input field")
	case 1:
		for k := range uniqueSet {
			return k, nil
		}
	default:
		return "", errors.New("inline edit action maps property '" + propertyName + "' from multiple input fields; configure inline_edit_config.input_name explicitly")
	}
	return "", nil
}

// validateInlineEditConfig mirrors `async fn validate_inline_edit_config`.
// Loads the referenced action_type, decodes it into the
// public model, and runs the four invariants from TASK L:
//
//  1. Same `object_type_id` as the property's owning type.
//  2. `operation_kind == "update_object"`.
//  3. The action's input schema carries an input field whose
//     `property_type` matches the property's type.
//  4. The action config does NOT enable side-effect notifications
//     or side-effect webhooks.
func validateInlineEditConfig(ctx context.Context, state *ontologykernel.AppState, objectTypeID uuid.UUID, propertyName, propertyType string, cfg *models.PropertyInlineEditConfig) error {
	row := state.DB.QueryRow(ctx,
		`SELECT id, name, display_name, description, object_type_id, operation_kind, input_schema,
                  form_schema, config, confirmation_required, permission_key, authorization_policy, owner_id,
                  created_at, updated_at
           FROM action_types
           WHERE id = $1`,
		cfg.ActionTypeID,
	)
	var atRow models.ActionTypeRow
	err := row.Scan(
		&atRow.ID, &atRow.Name, &atRow.DisplayName, &atRow.Description,
		&atRow.ObjectTypeID, &atRow.OperationKind,
		&atRow.InputSchema, &atRow.FormSchema, &atRow.Config,
		&atRow.ConfirmationRequired, &atRow.PermissionKey, &atRow.AuthorizationPolicy,
		&atRow.OwnerID, &atRow.CreatedAt, &atRow.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("configured inline edit action type was not found")
	}
	if err != nil {
		return errors.New("failed to load inline edit action type: " + err.Error())
	}
	action := atRow.IntoAction()

	if action.ObjectTypeID != objectTypeID {
		return errors.New("inline edit action must belong to the same object type as the property")
	}
	if action.OperationKind != "update_object" {
		return errors.New("inline edit action must use the update_object operation")
	}

	inputName, err := resolveInlineEditInputName(action, propertyName, cfg)
	if err != nil {
		return err
	}

	var inputField *models.ActionInputField
	for i := range action.InputSchema {
		if action.InputSchema[i].Name == inputName {
			inputField = &action.InputSchema[i]
			break
		}
	}
	if inputField == nil {
		return errors.New("inline edit action input field '" + inputName + "' was not found in the action schema")
	}
	if inputField.PropertyType != propertyType {
		return errors.New("inline edit action input '" + inputName + "' has type '" + inputField.PropertyType + "' but property '" + propertyName + "' has type '" + propertyType + "'")
	}
	return enforceInlineEditActionEnvelope(action.Config)
}

// enforceInlineEditActionEnvelope mirrors
// `fn enforce_inline_edit_action_envelope`. Rejects envelope
// features incompatible with inline edits:
// `webhook_side_effects` and `notification_side_effects` (writeback
// webhooks remain allowed).
func enforceInlineEditActionEnvelope(config json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(config, &obj); err != nil || obj == nil {
		return nil
	}
	if raw, ok := obj["notification_side_effects"]; ok && !isEmptyArrayOrNull(raw) {
		return errors.New("inline edit action types must not enable side-effect notifications")
	}
	if raw, ok := obj["webhook_side_effects"]; ok && !isEmptyArrayOrNull(raw) {
		return errors.New("inline edit action types must not enable side-effect webhooks")
	}
	return nil
}

// isEmptyArrayOrNull mirrors the Rust `is_empty || is_null` shortcut.
func isEmptyArrayOrNull(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" || trimmed == "[]" {
		return true
	}
	return false
}

// scanProperty reads a Property row off pgx.Row or pgx.Rows. The
// `inline_edit_config` JSONB column is decoded via a temp byte
// buffer (pgx doesn't auto-map JSONB to struct pointers).
func scanProperty(row interface{ Scan(...any) error }) (models.Property, error) {
	var (
		p          models.Property
		inlineRaw  []byte
	)
	if err := row.Scan(
		&p.ID, &p.ObjectTypeID, &p.Name, &p.DisplayName, &p.Description, &p.PropertyType,
		&p.Required, &p.UniqueConstraint, &p.TimeDependent,
		&p.DefaultValue, &p.ValidationRules,
		&inlineRaw,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return p, err
	}
	if len(inlineRaw) > 0 && string(inlineRaw) != "null" {
		var cfg models.PropertyInlineEditConfig
		if err := json.Unmarshal(inlineRaw, &cfg); err == nil {
			p.InlineEditConfig = &cfg
		}
	}
	return p, nil
}
