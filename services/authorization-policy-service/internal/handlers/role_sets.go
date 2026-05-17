// role_sets.go: SG.7 — HTTP surface for role sets, the operation
// catalog, and the delegation-rank checker.

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── role-sets ──────────────────────────────────────────────────────

func (h *Handlers) ListRoleSets(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "read")
	if !ok {
		return
	}
	ctxFilter := strings.TrimSpace(r.URL.Query().Get("context"))
	if ctxFilter != "" && !isAllowedRoleSetContext(ctxFilter) {
		writeJSONErr(w, http.StatusBadRequest, "context must be project, ontology, restricted_view, or platform_admin")
		return
	}
	items, err := h.Repo.ListRoleSets(r.Context(), tenantFromClaims(claims), ctxFilter)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) GetRoleSet(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "read")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	rs, err := h.Repo.GetRoleSet(r.Context(), tenantFromClaims(claims), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rs == nil {
		writeJSONErr(w, http.StatusNotFound, "role set not found")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (h *Handlers) CreateRoleSet(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "write")
	if !ok {
		return
	}
	var body models.CreateRoleSetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.Slug) == "" || strings.TrimSpace(body.Name) == "" {
		writeJSONErr(w, http.StatusBadRequest, "slug and name are required")
		return
	}
	if !isAllowedRoleSetContext(body.Context) {
		writeJSONErr(w, http.StatusBadRequest, "context must be project, ontology, restricted_view, or platform_admin")
		return
	}
	rs, err := h.Repo.CreateRoleSet(r.Context(), tenantFromClaims(claims), &body)
	if err != nil {
		writeRBACMutationErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rs)
}

func (h *Handlers) UpdateRoleSet(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.UpdateRoleSetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rs, err := h.Repo.UpdateRoleSet(r.Context(), tenantFromClaims(claims), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rs == nil {
		writeJSONErr(w, http.StatusNotFound, "role set not found")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (h *Handlers) DeleteRoleSet(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Repo.DeleteRoleSet(r.Context(), tenantFromClaims(claims), id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── role-set members ───────────────────────────────────────────────

func (h *Handlers) AddRoleToRoleSet(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "write")
	if !ok {
		return
	}
	roleSetID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.AddRoleToRoleSetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.RoleID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "role_id is required")
		return
	}
	if body.Rank <= 0 {
		writeJSONErr(w, http.StatusBadRequest, "rank must be a positive integer")
		return
	}
	rs, err := h.Repo.GetRoleSet(r.Context(), tenantFromClaims(claims), roleSetID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rs == nil {
		writeJSONErr(w, http.StatusNotFound, "role set not found")
		return
	}
	member, err := h.Repo.AddRoleToRoleSet(r.Context(), roleSetID, &body)
	if err != nil {
		writeRBACMutationErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, member)
}

func (h *Handlers) RemoveRoleFromRoleSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePermission(w, r, "roles", "write"); !ok {
		return
	}
	roleSetID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	roleID, ok := parseUUIDParam(w, r, "role_id")
	if !ok {
		return
	}
	if err := h.Repo.RemoveRoleFromRoleSet(r.Context(), roleSetID, roleID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── operation catalog ──────────────────────────────────────────────

func (h *Handlers) ListOperations(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "permissions", "read")
	if !ok {
		return
	}
	items, err := h.Repo.ListOperationCatalog(r.Context(), tenantFromClaims(claims))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListOperationsResponse{Items: items})
}

// ─── delegation-rank check ──────────────────────────────────────────

func (h *Handlers) CheckRoleSetDelegation(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "roles", "read")
	if !ok {
		return
	}
	roleSetID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.CheckDelegationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.TargetRoleID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "target_role_id is required")
		return
	}
	grantor := claims.Sub
	if body.GrantorID != nil {
		grantor = *body.GrantorID
	}
	resp, err := h.Repo.CheckDelegation(r.Context(), roleSetID, grantor, body.TargetRoleID)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── helpers ────────────────────────────────────────────────────────

func isAllowedRoleSetContext(c string) bool {
	switch c {
	case models.RoleSetContextProject,
		models.RoleSetContextOntology,
		models.RoleSetContextRestrictedView,
		models.RoleSetContextPlatformAdmin:
		return true
	default:
		return false
	}
}
