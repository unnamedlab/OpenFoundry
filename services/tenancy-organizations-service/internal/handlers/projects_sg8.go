// projects_sg8.go: SG.8 — role inheritance and direct grants.
//
// Adds:
//   - GET    /projects/{id}/resource-grants
//   - POST   /projects/{id}/resource-grants
//   - DELETE /projects/{id}/resource-grants/{grant_id}
//   - GET    /projects/{id}/effective-access?user_id=…&scope_kind=…&scope_id=…&group_ids=…
//
// The effective-access resolver composes the per-project sources of
// authority into a structured breakdown. Sources are ordered with
// the winning row first so callers can grab `sources[0]` for the
// effective role and walk the rest for the explanation.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// ─── resource grants CRUD ──────────────────────────────────────────

// ListProjectResourceGrants handles GET
// /api/v1/projects/{id}/resource-grants.
//
// Optional ?scope_kind / ?scope_id / ?principal_kind / ?principal_id
// query params filter the result.
func (h *ProjectsHandlers) ListProjectResourceGrants(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, projectID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	// View access is enough to see the grant list — admins / owners
	// see everything, regular members see grants on scopes they
	// can see anyway.
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, projectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	q := r.URL.Query()
	args := []any{projectID}
	conds := []string{"project_id = $1"}
	if v := strings.TrimSpace(q.Get("scope_kind")); v != "" {
		if v != models.ProjectGrantScopeProject && v != models.ProjectGrantScopeFolder {
			writeJSONErr(w, http.StatusBadRequest, "scope_kind must be 'project' or 'folder'")
			return
		}
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("scope_kind = $%d", len(args)))
	}
	if v := strings.TrimSpace(q.Get("scope_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "scope_id must be a uuid")
			return
		}
		args = append(args, id)
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)))
	}
	if v := strings.TrimSpace(q.Get("principal_kind")); v != "" {
		if v != models.ProjectGrantPrincipalUser && v != models.ProjectGrantPrincipalGroup {
			writeJSONErr(w, http.StatusBadRequest, "principal_kind must be 'user' or 'group'")
			return
		}
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("principal_kind = $%d", len(args)))
	}
	if v := strings.TrimSpace(q.Get("principal_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "principal_id must be a uuid")
			return
		}
		args = append(args, id)
		conds = append(conds, fmt.Sprintf("principal_id = $%d", len(args)))
	}
	query := `SELECT id, project_id, scope_kind, scope_id, principal_kind, principal_id, role,
	                 granted_by, created_at, updated_at
	          FROM ontology_project_resource_grants
	          WHERE ` + strings.Join(conds, " AND ") + `
	          ORDER BY created_at DESC LIMIT 500`
	rows, err := h.Pool.Query(r.Context(), query, args...)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := make([]models.ProjectResourceGrant, 0)
	for rows.Next() {
		g, scanErr := scanResourceGrant(rows)
		if scanErr != nil {
			writeJSONErr(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		items = append(items, *g)
	}
	writeJSON(w, http.StatusOK, models.ListProjectResourceGrantsResponse{Data: items})
}

// CreateProjectResourceGrant handles POST
// /api/v1/projects/{id}/resource-grants. Only project owner / admin
// can write.
func (h *ProjectsHandlers) CreateProjectResourceGrant(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, projectID)
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
	var body models.CreateProjectResourceGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !isAllowedGrantScopeKind(body.ScopeKind) {
		writeJSONErr(w, http.StatusBadRequest, "scope_kind must be 'project' or 'folder'")
		return
	}
	if !isAllowedGrantPrincipalKind(body.PrincipalKind) {
		writeJSONErr(w, http.StatusBadRequest, "principal_kind must be 'user' or 'group'")
		return
	}
	if body.PrincipalID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "principal_id is required")
		return
	}
	if _, err := models.ParseOntologyProjectRole(string(body.Role)); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// scope_kind=project ⇒ scope_id must be nil; scope_kind=folder ⇒
	// scope_id required AND must be a folder of this project.
	if body.ScopeKind == models.ProjectGrantScopeProject && body.ScopeID != nil {
		writeJSONErr(w, http.StatusBadRequest, "scope_id must be null for scope_kind 'project'")
		return
	}
	if body.ScopeKind == models.ProjectGrantScopeFolder {
		if body.ScopeID == nil {
			writeJSONErr(w, http.StatusBadRequest, "scope_id is required for scope_kind 'folder'")
			return
		}
		folder, err := loadProjectFolder(r.Context(), h.Pool, projectID, *body.ScopeID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if folder == nil {
			writeJSONErr(w, http.StatusBadRequest, "scope_id is not a folder of this project")
			return
		}
	}
	id := ids.New()
	now := time.Now().UTC()
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO ontology_project_resource_grants
		   (id, project_id, scope_kind, scope_id, principal_kind, principal_id, role, granted_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		 ON CONFLICT (project_id, scope_kind,
		              COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid),
		              principal_kind, principal_id)
		 DO UPDATE SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, updated_at = EXCLUDED.updated_at`,
		id, projectID, body.ScopeKind, body.ScopeID, body.PrincipalKind, body.PrincipalID,
		string(body.Role), claims.Sub, now,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	grant, err := loadResourceGrant(r.Context(), h, projectID, body.ScopeKind, body.ScopeID,
		body.PrincipalKind, body.PrincipalID)
	if err != nil || grant == nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to reload grant")
		return
	}
	writeJSON(w, http.StatusCreated, grant)
}

// DeleteProjectResourceGrant handles DELETE
// /api/v1/projects/{id}/resource-grants/{grant_id}.
func (h *ProjectsHandlers) DeleteProjectResourceGrant(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	grantID, err := uuid.Parse(chi.URLParam(r, "grant_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid grant_id")
		return
	}
	project, err := loadProject(r.Context(), h.Pool, projectID)
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
	tag, err := h.Pool.Exec(r.Context(),
		`DELETE FROM ontology_project_resource_grants WHERE id = $1 AND project_id = $2`,
		grantID, projectID,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSONErr(w, http.StatusNotFound, "grant not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── effective access resolver ─────────────────────────────────────

// CheckEffectiveAccess handles GET
// /api/v1/projects/{id}/effective-access. Anti-leak rule: the caller
// must be either the inspected user, the project owner, or a
// platform admin. Otherwise refuse with 403 without revealing the
// project's existence beyond what the caller already knew.
//
// Inputs (query params):
//
//	user_id      — required, UUID of the user to inspect.
//	scope_kind   — optional: 'project' (default) or 'folder'.
//	scope_id     — required when scope_kind = 'folder'.
//	group_ids    — optional comma-separated UUID list; supplied by
//	               the gateway from the user's JWT groups attribute
//	               because cross-service group-membership lookup
//	               lives in identity-federation-service.
func (h *ProjectsHandlers) CheckEffectiveAccess(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	q := r.URL.Query()
	rawUserID := strings.TrimSpace(q.Get("user_id"))
	if rawUserID == "" {
		writeJSONErr(w, http.StatusBadRequest, "user_id is required")
		return
	}
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "user_id must be a uuid")
		return
	}
	scopeKind := strings.TrimSpace(q.Get("scope_kind"))
	if scopeKind == "" {
		scopeKind = models.ProjectGrantScopeProject
	}
	if !isAllowedGrantScopeKind(scopeKind) {
		writeJSONErr(w, http.StatusBadRequest, "scope_kind must be 'project' or 'folder'")
		return
	}
	var scopeID *uuid.UUID
	if v := strings.TrimSpace(q.Get("scope_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "scope_id must be a uuid")
			return
		}
		scopeID = &parsed
	}
	if scopeKind == models.ProjectGrantScopeFolder && scopeID == nil {
		writeJSONErr(w, http.StatusBadRequest, "scope_id is required for scope_kind 'folder'")
		return
	}
	var groupIDs []uuid.UUID
	if v := strings.TrimSpace(q.Get("group_ids")); v != "" {
		for _, raw := range strings.Split(v, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			parsed, parseErr := uuid.Parse(raw)
			if parseErr != nil {
				writeJSONErr(w, http.StatusBadRequest, "group_ids must be a comma-separated list of uuids")
				return
			}
			groupIDs = append(groupIDs, parsed)
		}
	}
	// Anti-leak gate. The user can always inspect their own
	// effective access. Otherwise project owner / admin only.
	if claims.Sub != userID {
		project, err := loadProject(r.Context(), h.Pool, projectID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if project == nil {
			// Don't disclose whether the project exists — admins
			// see 404 because they may legitimately probe missing
			// projects; non-admins see 403 either way.
			if claims.HasRole("admin") {
				writeJSONErr(w, http.StatusNotFound, "ontology project not found")
			} else {
				writeJSONErr(w, http.StatusForbidden,
					"forbidden: only the inspected user, project owner, or platform admin can request effective-access")
			}
			return
		}
		if !claims.HasRole("admin") && project.OwnerID != claims.Sub {
			writeJSONErr(w, http.StatusForbidden,
				"forbidden: only the inspected user, project owner, or platform admin can request effective-access")
			return
		}
	}
	resp, err := resolveEffectiveAccess(r.Context(), h, projectID, userID, scopeKind, scopeID, groupIDs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveEffectiveAccess composes the per-project sources of
// authority for `userID` on `(scopeKind, scopeID)` into a structured
// breakdown. Implementation detail of the SG.8 handler; exported
// from a sibling file for testability of the resolution algorithm
// is not required because the per-source SQL is the test surface.
func resolveEffectiveAccess(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, userID uuid.UUID,
	scopeKind string,
	scopeID *uuid.UUID,
	groupIDs []uuid.UUID,
) (*models.EffectiveAccessResponse, error) {
	project, err := loadProject(ctx, h.Pool, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("ontology project not found")
	}
	resp := &models.EffectiveAccessResponse{
		UserID:    userID,
		ProjectID: projectID,
		ScopeKind: scopeKind,
		ScopeID:   scopeID,
		Sources:   []models.EffectiveAccessSource{},
		CheckedAt: time.Now().UTC(),
	}
	// 1. Project owner.
	if project.OwnerID == userID {
		resp.Sources = append(resp.Sources, models.EffectiveAccessSource{
			Kind: models.EffectiveAccessSourceProjectOwner,
			Role: models.OntologyProjectRoleOwner,
		})
	}
	// 2. Project default role (always contributes; rank floor).
	if project.DefaultRole != "" {
		resp.Sources = append(resp.Sources, models.EffectiveAccessSource{
			Kind: models.EffectiveAccessSourceProjectDefault,
			Role: project.DefaultRole,
		})
	}
	// 3. User-direct project membership.
	if role, ok, err := lookupUserProjectMembership(ctx, h, projectID, userID); err != nil {
		return nil, err
	} else if ok {
		resp.Sources = append(resp.Sources, models.EffectiveAccessSource{
			Kind:        models.EffectiveAccessSourceProjectUserMembership,
			Role:        role,
			PrincipalID: pointerOfUUID(userID),
		})
	}
	// 4. Group-via-project memberships (the SG.6 group grant table).
	if len(groupIDs) > 0 {
		groupGrants, err := lookupGroupProjectMemberships(ctx, h, projectID, groupIDs)
		if err != nil {
			return nil, err
		}
		for _, g := range groupGrants {
			gid := g.groupID
			resp.Sources = append(resp.Sources, models.EffectiveAccessSource{
				Kind:    models.EffectiveAccessSourceProjectGroupMembership,
				Role:    g.role,
				GroupID: &gid,
			})
		}
	}
	// 5. Direct resource grants — first scope='project', then
	//    scope='folder' for the requested folder *and* every
	//    ancestor up the parent chain.
	directRows, err := lookupResourceGrants(ctx, h, projectID, models.ProjectGrantScopeProject, nil, userID, groupIDs)
	if err != nil {
		return nil, err
	}
	for _, g := range directRows {
		gid := g.grantID
		var prinID *uuid.UUID
		var grpID *uuid.UUID
		if g.principalKind == models.ProjectGrantPrincipalUser {
			p := g.principalID
			prinID = &p
		} else {
			p := g.principalID
			grpID = &p
		}
		kind := models.EffectiveAccessSourceDirectUserGrant
		if g.principalKind == models.ProjectGrantPrincipalGroup {
			kind = models.EffectiveAccessSourceDirectGroupGrant
		}
		resp.Sources = append(resp.Sources, models.EffectiveAccessSource{
			Kind:        kind,
			Role:        g.role,
			GrantID:     &gid,
			PrincipalID: prinID,
			GroupID:     grpID,
		})
	}
	// 6. Folder grants — folder + ancestors when scopeKind = folder.
	if scopeKind == models.ProjectGrantScopeFolder && scopeID != nil {
		folderIDs, err := folderAncestry(ctx, h, projectID, *scopeID)
		if err != nil {
			return nil, err
		}
		for _, fid := range folderIDs {
			folder := fid
			folderGrants, err := lookupResourceGrants(ctx, h, projectID, models.ProjectGrantScopeFolder, &folder, userID, groupIDs)
			if err != nil {
				return nil, err
			}
			for _, g := range folderGrants {
				gid := g.grantID
				var prinID *uuid.UUID
				var grpID *uuid.UUID
				if g.principalKind == models.ProjectGrantPrincipalUser {
					p := g.principalID
					prinID = &p
				} else {
					p := g.principalID
					grpID = &p
				}
				kind := models.EffectiveAccessSourceFolderUserGrant
				if g.principalKind == models.ProjectGrantPrincipalGroup {
					kind = models.EffectiveAccessSourceFolderGroupGrant
				}
				resp.Sources = append(resp.Sources, models.EffectiveAccessSource{
					Kind:        kind,
					Role:        g.role,
					GrantID:     &gid,
					PrincipalID: prinID,
					GroupID:     grpID,
					FolderID:    &folder,
				})
			}
		}
	}
	// Resolve the winning role: highest rank across sources.
	for i := range resp.Sources {
		role := resp.Sources[i].Role
		if resp.ResolvedRole == nil || role.Rank() > resp.ResolvedRole.Rank() {
			r := role
			resp.ResolvedRole = &r
		}
	}
	// Sort sources so the winning row is first; ties keep insertion order.
	if len(resp.Sources) > 1 {
		stableSortSourcesDesc(resp.Sources)
	}
	return resp, nil
}

// ─── helpers ───────────────────────────────────────────────────────

func isAllowedGrantScopeKind(k string) bool {
	return k == models.ProjectGrantScopeProject || k == models.ProjectGrantScopeFolder
}

func isAllowedGrantPrincipalKind(k string) bool {
	return k == models.ProjectGrantPrincipalUser || k == models.ProjectGrantPrincipalGroup
}

func pointerOfUUID(id uuid.UUID) *uuid.UUID { return &id }

func scanResourceGrant(row interface{ Scan(...any) error }) (*models.ProjectResourceGrant, error) {
	g := &models.ProjectResourceGrant{}
	var roleStr string
	if err := row.Scan(&g.ID, &g.ProjectID, &g.ScopeKind, &g.ScopeID,
		&g.PrincipalKind, &g.PrincipalID, &roleStr,
		&g.GrantedBy, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	g.Role = models.OntologyProjectRole(roleStr)
	return g, nil
}

func loadResourceGrant(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID uuid.UUID,
	scopeKind string,
	scopeID *uuid.UUID,
	principalKind string,
	principalID uuid.UUID,
) (*models.ProjectResourceGrant, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT id, project_id, scope_kind, scope_id, principal_kind, principal_id, role,
		        granted_by, created_at, updated_at
		 FROM ontology_project_resource_grants
		 WHERE project_id = $1 AND scope_kind = $2
		   AND COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid)
		     = COALESCE($3, '00000000-0000-0000-0000-000000000000'::uuid)
		   AND principal_kind = $4 AND principal_id = $5`,
		projectID, scopeKind, scopeID, principalKind, principalID,
	)
	g, err := scanResourceGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

func lookupUserProjectMembership(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, userID uuid.UUID,
) (models.OntologyProjectRole, bool, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT role FROM ontology_project_memberships
		 WHERE project_id = $1 AND user_id = $2`,
		projectID, userID,
	)
	var role string
	if err := row.Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	parsed, err := models.ParseOntologyProjectRole(role)
	if err != nil {
		return "", false, err
	}
	return parsed, true, nil
}

type groupGrant struct {
	groupID uuid.UUID
	role    models.OntologyProjectRole
}

func lookupGroupProjectMemberships(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID uuid.UUID,
	groupIDs []uuid.UUID,
) ([]groupGrant, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT group_id, role
		 FROM ontology_project_group_memberships
		 WHERE project_id = $1 AND group_id = ANY($2::uuid[])`,
		projectID, groupIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]groupGrant, 0)
	for rows.Next() {
		g := groupGrant{}
		var roleStr string
		if err := rows.Scan(&g.groupID, &roleStr); err != nil {
			return nil, err
		}
		g.role = models.OntologyProjectRole(roleStr)
		out = append(out, g)
	}
	return out, rows.Err()
}

type resourceGrantHit struct {
	grantID       uuid.UUID
	principalKind string
	principalID   uuid.UUID
	role          models.OntologyProjectRole
}

func lookupResourceGrants(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID uuid.UUID,
	scopeKind string,
	scopeID *uuid.UUID,
	userID uuid.UUID,
	groupIDs []uuid.UUID,
) ([]resourceGrantHit, error) {
	// Build a SQL that matches the user (principal_kind='user') OR
	// any of the supplied group IDs (principal_kind='group').
	args := []any{projectID, scopeKind, userID}
	query := `SELECT id, principal_kind, principal_id, role
	          FROM ontology_project_resource_grants
	          WHERE project_id = $1 AND scope_kind = $2
	            AND COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid)
	              = COALESCE($` + sprintfPlaceholderForScope(scopeID, &args) + `, '00000000-0000-0000-0000-000000000000'::uuid)
	            AND (
	              (principal_kind = 'user' AND principal_id = $3)`
	if len(groupIDs) > 0 {
		args = append(args, groupIDs)
		query += fmt.Sprintf(" OR (principal_kind = 'group' AND principal_id = ANY($%d::uuid[]))", len(args))
	}
	query += ")"
	rows, err := h.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]resourceGrantHit, 0)
	for rows.Next() {
		hit := resourceGrantHit{}
		var roleStr string
		if err := rows.Scan(&hit.grantID, &hit.principalKind, &hit.principalID, &roleStr); err != nil {
			return nil, err
		}
		hit.role = models.OntologyProjectRole(roleStr)
		out = append(out, hit)
	}
	return out, rows.Err()
}

// sprintfPlaceholderForScope returns the placeholder index for the
// scope_id (or appends a fresh one) so the caller can interpolate it
// into the WHERE clause without duplicating the value.
func sprintfPlaceholderForScope(scopeID *uuid.UUID, args *[]any) string {
	*args = append(*args, scopeID)
	return fmt.Sprintf("%d", len(*args))
}

// folderAncestry walks the parent_folder_id chain up from `folderID`
// inclusive, returning [folderID, parent, grandparent, …].
func folderAncestry(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, folderID uuid.UUID,
) ([]uuid.UUID, error) {
	rows, err := h.Pool.Query(ctx,
		`WITH RECURSIVE ancestry AS (
		     SELECT id, parent_folder_id
		     FROM ontology_project_folders
		     WHERE project_id = $1 AND id = $2
		     UNION ALL
		     SELECT f.id, f.parent_folder_id
		     FROM ontology_project_folders f
		     INNER JOIN ancestry a ON a.parent_folder_id = f.id
		     WHERE f.project_id = $1
		 )
		 SELECT id FROM ancestry`,
		projectID, folderID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]uuid.UUID, 0, 4)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// stableSortSourcesDesc sorts in place so the highest-ranked source
// is first. Ties preserve original insertion order (insertion-sort).
func stableSortSourcesDesc(s []models.EffectiveAccessSource) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j].Role.Rank() > s[j-1].Role.Rank() {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}
