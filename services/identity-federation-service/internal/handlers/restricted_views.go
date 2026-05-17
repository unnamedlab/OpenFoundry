package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/restrictedview"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// RestrictedViews wires CRUD for the slice-7a CBAC restricted-view rows.
type RestrictedViews struct{ Auth *RBAC }

// NewRestrictedViews wraps an existing RBAC handler so the embedded
// repo + auth helpers don't need re-importing here.
func NewRestrictedViews(rbac *RBAC) *RestrictedViews { return &RestrictedViews{Auth: rbac} }

// List handles GET /api/v1/restricted-views.
func (h *RestrictedViews) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	views, err := h.Auth.Repo.ListRestrictedViews(r.Context(), tenantID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// Get handles GET /api/v1/restricted-views/{id}.
func (h *RestrictedViews) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	v, err := h.Auth.Repo.GetRestrictedView(r.Context(), id, tenantID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Create handles POST /api/v1/restricted-views.
func (h *RestrictedViews) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	caller := authCallerID(r)
	var body models.CreateRestrictedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := validateCreateRestrictedViewRequest(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.Auth.Repo.CreateRestrictedView(r.Context(), &body, caller, tenantID)
	if err != nil {
		slog.Error("create restricted view", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// Update handles PATCH /api/v1/restricted-views/{id}.
func (h *RestrictedViews) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.UpdateRestrictedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := validateUpdateRestrictedViewRequest(&body, r.Method == http.MethodPut); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.Auth.Repo.UpdateRestrictedView(r.Context(), id, tenantID, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Delete handles DELETE /api/v1/restricted-views/{id}.
func (h *RestrictedViews) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.Auth.Repo.DeleteRestrictedView(r.Context(), id, tenantID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tenantFromRequest resolves the caller's tenant (claims.OrgID). A
// 401 is emitted when the request is unauthenticated; a 403 when the
// authenticated caller has no organization, which would otherwise let
// them author or read restricted-views in the all-zero sentinel
// bucket that hosts orphaned, backfilled rows.
func tenantFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return uuid.Nil, false
	}
	if claims.OrgID == nil {
		writeJSONErr(w, http.StatusForbidden, "tenant required")
		return uuid.Nil, false
	}
	return *claims.OrgID, true
}

func validateCreateRestrictedViewRequest(body *models.CreateRestrictedViewRequest) error {
	body.Name = strings.TrimSpace(body.Name)
	body.Resource = strings.TrimSpace(body.Resource)
	body.Action = strings.TrimSpace(body.Action)
	if body.Name == "" || body.Resource == "" || body.Action == "" {
		return fmt.Errorf("name, resource and action required")
	}
	if body.Enabled == nil {
		return fmt.Errorf("enabled is required")
	}
	return validateRestrictedViewJSONFields(body.HiddenColumns, body.MarkingColumns, body.AllowedOrgIDs, body.AllowedMarkings, body.BackingDatasetSchema)
}

func validateUpdateRestrictedViewRequest(body *models.UpdateRestrictedViewRequest, requireFull bool) error {
	if requireFull {
		if body.Name == nil || body.Resource == nil || body.Action == nil || body.Enabled == nil {
			return fmt.Errorf("name, resource, action and enabled required")
		}
	}
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			return fmt.Errorf("name is required")
		}
		body.Name = &name
	}
	if body.Resource != nil {
		resource := strings.TrimSpace(*body.Resource)
		if resource == "" {
			return fmt.Errorf("resource is required")
		}
		body.Resource = &resource
	}
	if body.Action != nil {
		action := strings.TrimSpace(*body.Action)
		if action == "" {
			return fmt.Errorf("action is required")
		}
		body.Action = &action
	}
	return validateRestrictedViewJSONFields(body.HiddenColumns, body.MarkingColumns, body.AllowedOrgIDs, body.AllowedMarkings, body.BackingDatasetSchema)
}

func validateRestrictedViewJSONFields(hiddenColumns, markingColumns, allowedOrgIDs, allowedMarkings, backingDatasetSchema json.RawMessage) error {
	if err := validateRestrictedViewStringArray(hiddenColumns, "hidden_columns", false); err != nil {
		return err
	}
	if err := validateRestrictedViewStringArray(markingColumns, "marking_columns", false); err != nil {
		return err
	}
	if len(allowedOrgIDs) > 0 {
		var ids []uuid.UUID
		if err := json.Unmarshal(allowedOrgIDs, &ids); err != nil {
			return fmt.Errorf("allowed_org_ids must be an array of UUIDs")
		}
	}
	if err := validateRestrictedViewStringArray(allowedMarkings, "allowed_markings", true); err != nil {
		return err
	}
	return validateRestrictedViewBackingDatasetSchema(markingColumns, backingDatasetSchema)
}

func validateRestrictedViewStringArray(raw json.RawMessage, field string, validateMarkings bool) error {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s must be an array of strings", field)
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%s cannot contain empty values", field)
		}
		if validateMarkings && !isRestrictedViewMarkingID(trimmed) {
			return fmt.Errorf("invalid marking '%s', expected a marking UUID or one of [public confidential pii]", trimmed)
		}
	}
	return nil
}

func validateRestrictedViewBackingDatasetSchema(markingColumns, backingDatasetSchema json.RawMessage) error {
	if len(backingDatasetSchema) == 0 {
		return nil
	}
	fields, err := restrictedview.SchemaFromJSON(backingDatasetSchema)
	if err != nil {
		return err
	}
	columns := []string{}
	if len(markingColumns) > 0 {
		if err := json.Unmarshal(markingColumns, &columns); err != nil {
			return fmt.Errorf("marking_columns must be an array of strings")
		}
	}
	if errs := restrictedview.ValidateMarkingBackedSchema(fields, columns); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func isRestrictedViewMarkingID(value string) bool {
	if _, err := uuid.Parse(strings.TrimSpace(value)); err == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public", "confidential", "pii":
		return true
	default:
		return false
	}
}
