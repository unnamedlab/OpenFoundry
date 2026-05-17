package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── ABAC policies CRUD ─────────────────────────────────────────────

func validEffect(effect string) bool {
	return effect == "allow" || effect == "deny"
}

// requireTenant resolves the caller's tenant UUID from the auth context.
// Responds 401 when unauthenticated and 403 when the JWT does not carry
// org_id — there is no admin override: ABAC rows are tenant-scoped, a
// caller without a tenant has nothing to read.
func requireTenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return uuid.Nil, false
	}
	tenantID, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusForbidden, "tenant scope required")
		return uuid.Nil, false
	}
	return tenantID, true
}

// ListABACPolicies handles GET /api/v1/abac-policies.
func (h *Handlers) ListABACPolicies(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.Repo.ListABACPolicies(r.Context(), tenantID)
	if err != nil {
		slog.Error("list abac policies", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list policies")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ABACPolicy]{Items: items})
}

// GetABACPolicy handles GET /api/v1/abac-policies/{id}.
func (h *Handlers) GetABACPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := h.Repo.GetABACPolicy(r.Context(), tenantID, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// CreateABACPolicy handles POST /api/v1/abac-policies.
func (h *Handlers) CreateABACPolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tenantID, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusForbidden, "tenant scope required")
		return
	}
	var body models.CreateABACPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Resource = strings.TrimSpace(body.Resource)
	body.Action = strings.TrimSpace(body.Action)
	if body.Name == "" || body.Resource == "" || body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, "name, resource, and action required")
		return
	}
	if !validEffect(body.Effect) {
		writeJSONErr(w, http.StatusBadRequest, "effect must be 'allow' or 'deny'")
		return
	}
	if len(body.Conditions) > 0 && !json.Valid(body.Conditions) {
		writeJSONErr(w, http.StatusBadRequest, "conditions must be valid JSON")
		return
	}
	p, err := h.Repo.CreateABACPolicy(r.Context(), &body, tenantID, caller.Sub)
	if err != nil {
		slog.Error("create abac policy", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// UpdateABACPolicy handles PATCH /api/v1/abac-policies/{id}.
func (h *Handlers) UpdateABACPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.UpdateABACPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Effect != nil && !validEffect(*body.Effect) {
		writeJSONErr(w, http.StatusBadRequest, "effect must be 'allow' or 'deny'")
		return
	}
	if len(body.Conditions) > 0 && !json.Valid(body.Conditions) {
		writeJSONErr(w, http.StatusBadRequest, "conditions must be valid JSON")
		return
	}
	p, err := h.Repo.UpdateABACPolicy(r.Context(), tenantID, id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// DeleteABACPolicy handles DELETE /api/v1/abac-policies/{id}.
func (h *Handlers) DeleteABACPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteABACPolicy(r.Context(), tenantID, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
