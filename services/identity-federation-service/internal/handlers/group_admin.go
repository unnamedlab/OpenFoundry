// group_admin.go: SG.5 — group administration endpoints layered on
// top of the existing RBAC group CRUD.
//
// Adds:
//   - GET    /api/v1/groups/search           (kind / realm / org / status filter)
//   - GET    /api/v1/groups/{id}/inspect     (combined view)
//   - GET    /api/v1/groups/{id}/members     (with expires_at)
//   - GET    /api/v1/groups/{id}/admins
//   - POST   /api/v1/groups/{id}/admins
//   - DELETE /api/v1/groups/{id}/admins/{user_id}
//   - GET    /api/v1/groups/{id}/parents
//   - GET    /api/v1/groups/{id}/children
//   - PUT    /api/v1/groups/{id}/nested/{member_id}
//   - DELETE /api/v1/groups/{id}/nested/{member_id}

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

const projectAccessHintMessage = "Direct and inherited project access lives in tenancy-organizations-service: GET /api/v1/projects?group_id=<id>"

// SearchGroups implements GET /api/v1/groups/search with SG.5 filters.
func (h *RBAC) SearchGroups(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := &models.ListGroupsFilter{
		Query:  strings.TrimSpace(q.Get("q")),
		Kind:   strings.TrimSpace(q.Get("kind")),
		Realm:  strings.TrimSpace(q.Get("realm")),
		Status: strings.TrimSpace(q.Get("status")),
		Limit:  parseIntDefault(q.Get("limit"), 50),
		Offset: parseIntDefault(q.Get("offset"), 0),
	}
	if filter.Kind != "" && !isAllowedGroupKind(filter.Kind) {
		writeJSONErr(w, http.StatusBadRequest, "kind must be 'internal', 'external', or 'rule_based'")
		return
	}
	if filter.Status != "" && filter.Status != "active" && filter.Status != "archived" {
		writeJSONErr(w, http.StatusBadRequest, "status must be 'active' or 'archived'")
		return
	}
	if raw := strings.TrimSpace(q.Get("organization_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid organization_id")
			return
		}
		filter.OrganizationID = &parsed
	}

	items, total, err := h.Repo.ListGroupsFiltered(r.Context(), filter)
	if err != nil {
		slog.Error("search groups", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, models.ListGroupsResponse{Items: items, Total: total})
}

// InspectGroup returns the SG.5 combined view: group core + counts +
// admins + nested parents/children + a pointer to project-access
// lookups (which live in tenancy-organizations-service).
func (h *RBAC) InspectGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	group, err := h.Repo.GetGroup(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if group == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	direct, expiring, err := h.Repo.CountGroupMembers(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "count members: "+err.Error())
		return
	}
	admins, err := h.Repo.ListGroupAdmins(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "list admins: "+err.Error())
		return
	}
	parents, err := h.Repo.ListGroupParents(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "list parents: "+err.Error())
		return
	}
	children, err := h.Repo.ListGroupChildren(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "list children: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.GroupInspection{
		Group:               *group,
		DirectMemberCount:   direct,
		ExpiringMemberCount: expiring,
		Admins:              admins,
		Parents:             parents,
		Children:            children,
		ProjectAccessHint:   projectAccessHintMessage,
	})
}

// ListGroupMembers handles GET /api/v1/groups/{id}/members.
func (h *RBAC) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	items, err := h.Repo.ListGroupMembers(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// ─── Group admins ──────────────────────────────────────────────────────

func (h *RBAC) ListGroupAdmins(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	admins, err := h.Repo.ListGroupAdmins(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, admins)
}

func (h *RBAC) AddGroupAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.CreateGroupAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.UserID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if body.Scope != nil {
		s := strings.ToLower(strings.TrimSpace(*body.Scope))
		if s != models.GroupAdminScopeManage && s != models.GroupAdminScopeManageMembers {
			writeJSONErr(w, http.StatusBadRequest, "scope must be 'manage' or 'manage_members'")
			return
		}
	}
	admin, err := h.Repo.UpsertGroupAdmin(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, admin)
}

func (h *RBAC) RemoveGroupAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = models.GroupAdminScopeManage
	}
	if err := h.Repo.DeleteGroupAdmin(r.Context(), id, userID, scope); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Nested groups ─────────────────────────────────────────────────────

func (h *RBAC) ListGroupParents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	items, err := h.Repo.ListGroupParents(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *RBAC) ListGroupChildren(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	items, err := h.Repo.ListGroupChildren(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *RBAC) AddNestedGroup(w http.ResponseWriter, r *http.Request) {
	parentID, ok := parseID(w, r)
	if !ok {
		return
	}
	memberID, err := uuid.Parse(chi.URLParam(r, "member_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid member_id")
		return
	}
	var addedBy *uuid.UUID
	if caller := authCallerID(r); caller != uuid.Nil {
		c := caller
		addedBy = &c
	}
	if err := h.Repo.AddGroupNested(r.Context(), parentID, memberID, addedBy); err != nil {
		// Cycle / self-reference rejections surface as 400.
		if strings.Contains(err.Error(), "cycle") || strings.Contains(err.Error(), "itself") {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) RemoveNestedGroup(w http.ResponseWriter, r *http.Request) {
	parentID, ok := parseID(w, r)
	if !ok {
		return
	}
	memberID, err := uuid.Parse(chi.URLParam(r, "member_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid member_id")
		return
	}
	if err := h.Repo.RemoveGroupNested(r.Context(), parentID, memberID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isAllowedGroupKind(k string) bool {
	switch k {
	case models.GroupKindInternal, models.GroupKindExternal, models.GroupKindRuleBased:
		return true
	default:
		return false
	}
}
