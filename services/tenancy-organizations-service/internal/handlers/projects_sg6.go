// projects_sg6.go: SG.6 — project security-boundary admin surface
// (group memberships, access requests, group setup shortcut).
//
// These handlers live next to projects.go so they share its helpers
// (parseUUIDParam, authClaims, ensureProjectOwnerOrAdmin, loadProject).
// Routes register under /api/v1 in server.go.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// ─── Group memberships ─────────────────────────────────────────────────

// ListProjectGroupMemberships handles
// GET /api/v1/projects/{id}/group-memberships.
//
// SG.6: lets admins see which groups have a role on the project. Read
// requires the same project-view access as the user-membership list.
func (h *ProjectsHandlers) ListProjectGroupMemberships(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	items, err := listProjectGroupMemberships(r.Context(), h, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListOntologyProjectGroupMembershipsResponse{Data: items})
}

// UpsertProjectGroupMembership handles
// PUT /api/v1/projects/{id}/group-memberships.
func (h *ProjectsHandlers) UpsertProjectGroupMembership(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	var body models.UpsertProjectGroupMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.GroupID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "group_id is required")
		return
	}
	role, err := models.ParseOntologyProjectRole(string(body.Role))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := upsertProjectGroupMembership(r.Context(), h, id, body.GroupID, role, &claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteProjectGroupMembership handles
// DELETE /api/v1/projects/{id}/group-memberships/{group_id}.
func (h *ProjectsHandlers) DeleteProjectGroupMembership(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "group_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid group_id")
		return
	}
	project, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	if _, err := h.Pool.Exec(r.Context(),
		`DELETE FROM ontology_project_group_memberships
		 WHERE project_id = $1 AND group_id = $2`,
		id, groupID,
	); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EnsureProjectAccessGroups handles
// POST /api/v1/projects/{id}/access-groups:bootstrap.
//
// SG.6: "Provide viewer/editor/owner group setup shortcuts." The
// caller supplies the three group IDs (auto-creation of groups lives
// in identity-federation-service); this handler upserts the three
// project-group bindings in one transaction.
func (h *ProjectsHandlers) EnsureProjectAccessGroups(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	var body models.EnsureProjectAccessGroupsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.ViewerGroupID == nil && body.EditorGroupID == nil && body.OwnerGroupID == nil {
		writeJSONErr(w, http.StatusBadRequest,
			"at least one of viewer_group_id, editor_group_id, owner_group_id is required")
		return
	}
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback(context.Background())

	bind := func(g *uuid.UUID, role models.OntologyProjectRole) (*models.OntologyProjectGroupMembership, error) {
		if g == nil {
			return nil, nil
		}
		row := tx.QueryRow(r.Context(),
			`INSERT INTO ontology_project_group_memberships
			   (project_id, group_id, role, granted_by)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (project_id, group_id) DO UPDATE
			   SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, updated_at = NOW()
			 RETURNING project_id, group_id, role, granted_by, created_at, updated_at`,
			id, *g, string(role), claims.Sub,
		)
		out := &models.OntologyProjectGroupMembership{}
		var roleStr string
		if err := row.Scan(&out.ProjectID, &out.GroupID, &roleStr, &out.GrantedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return nil, fmt.Errorf("bind %s group: %w", role, err)
		}
		out.Role = models.OntologyProjectRole(roleStr)
		return out, nil
	}

	resp := models.EnsureProjectAccessGroupsResponse{}
	if resp.Viewer, err = bind(body.ViewerGroupID, models.OntologyProjectRoleViewer); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resp.Editor, err = bind(body.EditorGroupID, models.OntologyProjectRoleEditor); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resp.Owner, err = bind(body.OwnerGroupID, models.OntologyProjectRoleOwner); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── Access requests ───────────────────────────────────────────────────

// CreateProjectAccessRequest handles
// POST /api/v1/projects/{id}/access-requests.
//
// SG.6: "Ensure file/folder requests inside a project resolve to
// project-level access requests." A folder/file request is just an
// access request with scope_resource_kind set to "folder"/"file" — the
// decision still happens at the project level.
func (h *ProjectsHandlers) CreateProjectAccessRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	var body models.CreateProjectAccessRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req, err := createAccessRequestWorkflow(r.Context(), h, project, claims.Sub, &body)
	if err != nil {
		writeAccessWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

// ListProjectAccessRequests handles
// GET /api/v1/projects/{id}/access-requests.
//
// Owners / admins see every request; everyone else only sees their
// own. Filter by ?status= (pending|approved|denied|cancelled).
func (h *ProjectsHandlers) ListProjectAccessRequests(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	isAdmin := claims.HasRole("admin") || project.OwnerID == claims.Sub
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !isAllowedAccessRequestStatus(status) {
		writeJSONErr(w, http.StatusBadRequest, "status must be pending, approved, denied, or cancelled")
		return
	}
	args := []any{id}
	query := `SELECT ` + accessRequestSelectColumns("") + `
	          FROM ontology_project_access_requests
	          WHERE project_id = $1`
	if !isAdmin {
		args = append(args, claims.Sub)
		query += fmt.Sprintf(" AND requested_by = $%d", len(args))
	}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	query += " ORDER BY created_at DESC LIMIT 200"
	rows, err := h.Pool.Query(r.Context(), query, args...)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := make([]models.OntologyProjectAccessRequest, 0)
	for rows.Next() {
		req, scanErr := scanAccessRequest(rows)
		if scanErr != nil {
			writeJSONErr(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		if err := attachAccessRequestTasks(r.Context(), h, req); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, *req)
	}
	writeJSON(w, http.StatusOK, models.ListOntologyProjectAccessRequestsResponse{Data: items})
}

// DecideProjectAccessRequest handles
// POST /api/v1/projects/{id}/access-requests/{request_id}/decision.
//
// On approve, the upgrade is *not* automatic — the handler records
// the decision and the granted role is materialised via the existing
// PUT /projects/{id}/memberships endpoint. This keeps the audit
// trail and the actual grant decoupled.
func (h *ProjectsHandlers) DecideProjectAccessRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	reqID, err := uuid.Parse(chi.URLParam(r, "request_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid request_id")
		return
	}
	project, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	var body models.DecideProjectAccessRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req, err := decideAccessRequestWorkflow(r.Context(), h, project, claims, reqID, &body)
	if err != nil {
		writeAccessWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// CancelProjectAccessRequest handles
// POST /api/v1/projects/{id}/access-requests/{request_id}:cancel.
// Only the requester can cancel; the request must be pending.
func (h *ProjectsHandlers) CancelProjectAccessRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	reqID, err := uuid.Parse(chi.URLParam(r, "request_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid request_id")
		return
	}
	tag, err := h.Pool.Exec(r.Context(),
		`UPDATE ontology_project_access_requests
		 SET status = 'cancelled', decided_at = NOW()
		 WHERE id = $1 AND project_id = $2
		   AND requested_by = $3 AND status = 'pending'`,
		reqID, id, claims.Sub,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSONErr(w, http.StatusConflict, "request not found, not yours, or not pending")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ───────────────────────────────────────────────────────────

func listProjectGroupMemberships(ctx context.Context, h *ProjectsHandlers, projectID uuid.UUID) ([]models.OntologyProjectGroupMembership, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT project_id, group_id, role, granted_by, created_at, updated_at
		 FROM ontology_project_group_memberships
		 WHERE project_id = $1
		 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.OntologyProjectGroupMembership, 0)
	for rows.Next() {
		m := models.OntologyProjectGroupMembership{}
		var roleStr string
		if err := rows.Scan(&m.ProjectID, &m.GroupID, &roleStr, &m.GrantedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Role = models.OntologyProjectRole(roleStr)
		out = append(out, m)
	}
	return out, rows.Err()
}

func upsertProjectGroupMembership(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, groupID uuid.UUID,
	role models.OntologyProjectRole,
	grantedBy *uuid.UUID,
) (*models.OntologyProjectGroupMembership, error) {
	row := h.Pool.QueryRow(ctx,
		`INSERT INTO ontology_project_group_memberships
		   (project_id, group_id, role, granted_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id, group_id) DO UPDATE
		   SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, updated_at = NOW()
		 RETURNING project_id, group_id, role, granted_by, created_at, updated_at`,
		projectID, groupID, string(role), grantedBy,
	)
	out := &models.OntologyProjectGroupMembership{}
	var roleStr string
	if err := row.Scan(&out.ProjectID, &out.GroupID, &roleStr, &out.GrantedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	out.Role = models.OntologyProjectRole(roleStr)
	return out, nil
}

func loadProjectAccessRequest(ctx context.Context, h *ProjectsHandlers, id uuid.UUID) (*models.OntologyProjectAccessRequest, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT `+accessRequestSelectColumns("")+`
		 FROM ontology_project_access_requests
		 WHERE id = $1`,
		id,
	)
	req, err := scanAccessRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := attachAccessRequestTasks(ctx, h, req); err != nil {
		return nil, err
	}
	return req, nil
}

// accessRequestScannable lets scanAccessRequest accept both pgx.Row
// and pgx.Rows.
type accessRequestScannable interface {
	Scan(dest ...any) error
}

func scanAccessRequest(row accessRequestScannable) (*models.OntologyProjectAccessRequest, error) {
	req := &models.OntologyProjectAccessRequest{}
	var roleStr string
	var requestedForRaw []byte
	if err := row.Scan(
		&req.ID, &req.ProjectID, &req.RequestedBy, &req.RequestType, &requestedForRaw, &roleStr, &req.Reason,
		&req.ScopeResourceKind, &req.ScopeResourceID, &req.Status,
		&req.DecidedBy, &req.DecisionReason, &req.CreatedAt, &req.DecidedAt, &req.CompletedAt,
	); err != nil {
		return nil, err
	}
	req.RequestedRole = models.OntologyProjectRole(roleStr)
	requestedFor, err := uuidListFromJSON(requestedForRaw)
	if err != nil {
		return nil, fmt.Errorf("decode requested_for_user_ids: %w", err)
	}
	req.RequestedForUserIDs = requestedFor
	return req, nil
}

func isAllowedAccessRequestStatus(s string) bool {
	switch s {
	case models.ProjectAccessRequestStatusPending,
		models.ProjectAccessRequestStatusApproved,
		models.ProjectAccessRequestStatusDenied,
		models.ProjectAccessRequestStatusCancelled,
		models.ProjectAccessRequestStatusChangesRequested,
		models.ProjectAccessRequestStatusActionRequired,
		models.ProjectAccessRequestStatusCompleted:
		return true
	default:
		return false
	}
}
