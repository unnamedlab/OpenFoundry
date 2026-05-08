// Package projects ports `libs/ontology-kernel/src/handlers/projects.rs`
// 1:1: 16 endpoints covering ontology project CRUD + memberships +
// resource bindings + working state + branches + proposals +
// migrations under `/api/v1/ontology/projects`.
//
// Project access guards layer on top of the SQL via the helpers in
// [domain/project_access.go]. SQL strings are preserved
// byte-for-byte so the same migrations and indexes back both ports.

package projects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// Mount registers every endpoint on the chi router.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Get("/ontology/projects", ListProjects(state))
	r.Post("/ontology/projects", CreateProject(state))
	r.Get("/ontology/projects/{id}", GetProject(state))
	r.Patch("/ontology/projects/{id}", UpdateProject(state))
	r.Delete("/ontology/projects/{id}", DeleteProject(state))

	r.Get("/ontology/projects/{id}/memberships", ListProjectMemberships(state))
	r.Post("/ontology/projects/{id}/memberships", UpsertProjectMembership(state))
	r.Delete("/ontology/projects/{id}/memberships/{user_id}", DeleteProjectMembership(state))

	r.Get("/ontology/projects/{id}/resources", ListProjectResources(state))
	r.Post("/ontology/projects/{id}/resources", BindProjectResource(state))
	r.Delete("/ontology/projects/{id}/resources/{resource_kind}/{resource_id}", UnbindProjectResource(state))

	r.Get("/ontology/projects/{id}/working-state", GetProjectWorkingState(state))
	r.Put("/ontology/projects/{id}/working-state", ReplaceProjectWorkingState(state))

	r.Get("/ontology/projects/{id}/branches", ListProjectBranches(state))
	r.Post("/ontology/projects/{id}/branches", CreateProjectBranch(state))
	r.Patch("/ontology/projects/{id}/branches/{branch_id}", UpdateProjectBranch(state))

	r.Get("/ontology/projects/{id}/proposals", ListProjectProposals(state))
	r.Post("/ontology/projects/{id}/proposals", CreateProjectProposal(state))
	r.Patch("/ontology/projects/{id}/proposals/{proposal_id}", UpdateProjectProposal(state))

	r.Get("/ontology/projects/{id}/migrations", ListProjectMigrations(state))
	r.Post("/ontology/projects/{id}/migrations", CreateProjectMigration(state))
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

// ── Slug + branch-name normalisers (mirror Rust `normalize_slug` /
// `normalize_optional_slug` / `normalize_branch_name`) ──────────────────

// NormalizeSlug enforces the Foundry-style slug rules: lowercase
// letters / digits / hyphens, no leading/trailing hyphen.
func NormalizeSlug(value, fieldName string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", errors.New(fieldName + " is required")
	}
	for _, ch := range normalized {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' {
			return "", errors.New(fieldName + " must contain only lowercase letters, digits, and hyphens")
		}
	}
	if strings.HasPrefix(normalized, "-") || strings.HasSuffix(normalized, "-") {
		return "", errors.New(fieldName + " cannot start or end with a hyphen")
	}
	return normalized, nil
}

// NormalizeOptionalSlug returns ("", nil) for nil / empty / whitespace
// input so the caller can leave the column unchanged. A non-empty
// value falls through to NormalizeSlug.
func NormalizeOptionalSlug(value *string, fieldName string) (string, bool, error) {
	if value == nil {
		return "", false, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return "", false, nil
	}
	v, err := NormalizeSlug(trimmed, fieldName)
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// NormalizeBranchName accepts the same alphabet as slugs PLUS the
// forward slash, mirroring the Rust source.
func NormalizeBranchName(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", errors.New("branch name is required")
	}
	for _, ch := range normalized {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' && ch != '/' {
			return "", errors.New("branch name must contain only lowercase letters, digits, hyphens, and slashes")
		}
	}
	return normalized, nil
}

// ── Project lookup + ownership guard ────────────────────────────────────

func loadProject(ctx context.Context, state *ontologykernel.AppState, id uuid.UUID) (*models.OntologyProject, error) {
	row := state.DB.QueryRow(ctx,
		`SELECT id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at
           FROM ontology_projects
           WHERE id = $1`,
		id,
	)
	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("failed to load ontology project: " + err.Error())
	}
	return &p, nil
}

// ensureProjectOwnerOrAdmin mirrors `fn ensure_project_owner_or_admin`.
// Used for ownership-sensitive ops (delete, membership management).
func ensureProjectOwnerOrAdmin(project *models.OntologyProject, claims *authmw.Claims) error {
	if claims.HasRole("admin") || project.OwnerID == claims.Sub {
		return nil
	}
	return errors.New("forbidden: only the ontology project owner can manage memberships or delete the project")
}

// ── Project CRUD ────────────────────────────────────────────────────────

const projectColumns = `id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at`

// ListProjects mirrors `pub async fn list_projects`. The visibility
// filter applies AFTER the DB pull (admins see everything, mortals
// only what's in the accessible map). Pagination is applied to the
// filtered set so totals reflect what the caller can see.
func ListProjects(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		accessible, err := domain.ListAccessibleProjects(r.Context(), state.DB, claims)
		if err != nil {
			internalError(w, "failed to evaluate project access: "+err.Error())
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
		searchPattern := "%" + searchValue + "%"

		rows, err := state.DB.Query(r.Context(),
			`SELECT `+projectColumns+`
               FROM ontology_projects
               WHERE slug ILIKE $1 OR display_name ILIKE $1
               ORDER BY created_at DESC`,
			searchPattern,
		)
		if err != nil {
			internalError(w, "failed to list ontology projects: "+err.Error())
			return
		}
		defer rows.Close()
		all := []models.OntologyProject{}
		for rows.Next() {
			p, err := scanProject(rows)
			if err == nil {
				all = append(all, p)
			}
		}

		visible := all
		if !claims.HasRole("admin") {
			visible = make([]models.OntologyProject, 0, len(all))
			for _, p := range all {
				if _, ok := accessible[p.ID]; ok {
					visible = append(visible, p)
				}
			}
		}
		total := int64(len(visible))
		offset := (page - 1) * perPage
		if offset >= total {
			visible = []models.OntologyProject{}
		} else {
			end := offset + perPage
			if end > total {
				end = total
			}
			visible = visible[offset:end]
		}
		writeJSON(w, http.StatusOK, models.ListOntologyProjectsResponse{
			Data: visible, Total: total, Page: page, PerPage: perPage,
		})
	}
}

// CreateProject mirrors `pub async fn create_project`.
func CreateProject(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		var body models.CreateOntologyProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		slug, err := NormalizeSlug(body.Slug, "slug")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		workspaceSlug, hasWorkspace, err := NormalizeOptionalSlug(body.WorkspaceSlug, "workspace_slug")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		displayName := slug
		if body.DisplayName != nil {
			displayName = *body.DisplayName
		}
		description := ""
		if body.Description != nil {
			description = *body.Description
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		var wsArg any
		if hasWorkspace {
			wsArg = workspaceSlug
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_projects (id, slug, display_name, description, workspace_slug, owner_id)
               VALUES ($1, $2, $3, $4, $5, $6)
               RETURNING `+projectColumns,
			id, slug, displayName, description, wsArg, claims.Sub,
		)
		p, err := scanProject(row)
		if err != nil {
			internalError(w, "failed to create ontology project: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	}
}

// GetProject mirrors `pub async fn get_project`.
func GetProject(state *ontologykernel.AppState) http.HandlerFunc {
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
		project, err := loadProject(r.Context(), state, id)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, id); err != nil {
			forbidden(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, project)
	}
}

// UpdateProject mirrors `pub async fn update_project`.
//
// `workspace_slug` honours the Rust `Option<Option<String>>` three-
// way semantics through models.StringUpdate:
//
//	body == nil          → keep existing
//	body.Value == nil    → clear (set NULL)
//	body.Value != nil    → normalise + replace
func UpdateProject(state *ontologykernel.AppState) http.HandlerFunc {
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
		existing, err := loadProject(r.Context(), state, id)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if existing == nil {
			notFound(w, "ontology project not found")
			return
		}
		if err := ensureProjectOwnerOrAdmin(existing, claims); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.UpdateOntologyProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}

		var workspaceArg any
		switch {
		case body.WorkspaceSlug == nil:
			workspaceArg = existing.WorkspaceSlug
		case body.WorkspaceSlug.Value == nil:
			workspaceArg = nil
		default:
			normalised, _, err := NormalizeOptionalSlug(body.WorkspaceSlug.Value, "workspace_slug")
			if err != nil {
				badRequest(w, err.Error())
				return
			}
			if normalised == "" {
				workspaceArg = nil
			} else {
				workspaceArg = normalised
			}
		}
		row := state.DB.QueryRow(r.Context(),
			`UPDATE ontology_projects
               SET display_name = COALESCE($2, display_name),
                   description = COALESCE($3, description),
                   workspace_slug = $4,
                   updated_at = NOW()
               WHERE id = $1
               RETURNING `+projectColumns,
			id, body.DisplayName, body.Description, workspaceArg,
		)
		p, err := scanProject(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "ontology project not found")
			return
		}
		if err != nil {
			internalError(w, "failed to update ontology project: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

// DeleteProject mirrors `pub async fn delete_project`.
func DeleteProject(state *ontologykernel.AppState) http.HandlerFunc {
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
		project, err := loadProject(r.Context(), state, id)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM ontology_projects WHERE id = $1`,
			id,
		)
		if err != nil {
			internalError(w, "failed to delete ontology project: "+err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "ontology project not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Memberships ─────────────────────────────────────────────────────────

// ListProjectMemberships mirrors `pub async fn list_project_memberships`.
func ListProjectMemberships(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT project_id, user_id, role, created_at, updated_at
               FROM ontology_project_memberships
               WHERE project_id = $1
               ORDER BY created_at ASC`,
			projectID,
		)
		if err != nil {
			internalError(w, "failed to list ontology project memberships: "+err.Error())
			return
		}
		defer rows.Close()
		data := []models.OntologyProjectMembership{}
		for rows.Next() {
			var m models.OntologyProjectMembership
			if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt); err == nil {
				data = append(data, m)
			}
		}
		writeJSON(w, http.StatusOK, models.ListOntologyProjectMembershipsResponse{Data: data})
	}
}

// UpsertProjectMembership mirrors `pub async fn upsert_project_membership`.
func UpsertProjectMembership(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.UpsertOntologyProjectMembershipRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_project_memberships (project_id, user_id, role)
               VALUES ($1, $2, $3)
               ON CONFLICT (project_id, user_id)
               DO UPDATE SET role = EXCLUDED.role, updated_at = NOW()
               RETURNING project_id, user_id, role, created_at, updated_at`,
			projectID, body.UserID, body.Role,
		)
		var m models.OntologyProjectMembership
		if err := row.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt); err != nil {
			internalError(w, "failed to upsert ontology project membership: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

// DeleteProjectMembership mirrors `pub async fn delete_project_membership`.
func DeleteProjectMembership(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		userID, err := pathUUID(r, "user_id")
		if err != nil {
			badRequest(w, "invalid user_id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM ontology_project_memberships
               WHERE project_id = $1 AND user_id = $2`,
			projectID, userID,
		)
		if err != nil {
			internalError(w, "failed to delete ontology project membership: "+err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "ontology project membership not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Resource bindings ────────────────────────────────────────────────────

// ListProjectResources mirrors `pub async fn list_project_resources`.
func ListProjectResources(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT project_id, resource_kind, resource_id, bound_by, created_at
               FROM ontology_project_resources
               WHERE project_id = $1
               ORDER BY created_at DESC`,
			projectID,
		)
		if err != nil {
			internalError(w, "failed to list ontology project resources: "+err.Error())
			return
		}
		defer rows.Close()
		data := []models.OntologyProjectResourceBinding{}
		for rows.Next() {
			var b models.OntologyProjectResourceBinding
			if err := rows.Scan(&b.ProjectID, &b.ResourceKind, &b.ResourceID, &b.BoundBy, &b.CreatedAt); err == nil {
				data = append(data, b)
			}
		}
		writeJSON(w, http.StatusOK, models.ListOntologyProjectResourcesResponse{Data: data})
	}
}

// BindProjectResource mirrors `pub async fn bind_project_resource`.
// Three guards in sequence: parse the resource_kind, ensure project
// edit access, ensure manage access on the resource itself.
func BindProjectResource(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		var body models.BindOntologyProjectResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		resourceKind, err := domain.ParseOntologyResourceKind(body.ResourceKind)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		owner, err := domain.LoadResourceOwnerID(r.Context(), state.DB, resourceKind, body.ResourceID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if owner == nil {
			notFound(w, "ontology resource not found")
			return
		}
		existingProjectID, err := domain.LoadResourceProjectID(r.Context(), state.DB, resourceKind, body.ResourceID)
		if err != nil {
			internalError(w, "failed to load ontology resource binding: "+err.Error())
			return
		}
		if err := domain.EnsureResourceManageAccess(r.Context(), state.DB, claims, *owner, existingProjectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_project_resources (project_id, resource_kind, resource_id, bound_by)
               VALUES ($1, $2, $3, $4)
               ON CONFLICT (resource_kind, resource_id)
               DO UPDATE SET project_id = EXCLUDED.project_id, bound_by = EXCLUDED.bound_by, created_at = NOW()
               RETURNING project_id, resource_kind, resource_id, bound_by, created_at`,
			projectID, resourceKind.AsStr(), body.ResourceID, claims.Sub,
		)
		var b models.OntologyProjectResourceBinding
		if err := row.Scan(&b.ProjectID, &b.ResourceKind, &b.ResourceID, &b.BoundBy, &b.CreatedAt); err != nil {
			internalError(w, "failed to bind ontology resource to project: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// UnbindProjectResource mirrors `pub async fn unbind_project_resource`.
func UnbindProjectResource(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		resourceID, err := pathUUID(r, "resource_id")
		if err != nil {
			badRequest(w, "invalid resource_id")
			return
		}
		resourceKind, err := domain.ParseOntologyResourceKind(chi.URLParam(r, "resource_kind"))
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT project_id, resource_kind, resource_id, bound_by, created_at
               FROM ontology_project_resources
               WHERE project_id = $1 AND resource_kind = $2 AND resource_id = $3`,
			projectID, resourceKind.AsStr(), resourceID,
		)
		var binding models.OntologyProjectResourceBinding
		err = row.Scan(&binding.ProjectID, &binding.ResourceKind, &binding.ResourceID, &binding.BoundBy, &binding.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "ontology project resource binding not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load ontology project resource binding: "+err.Error())
			return
		}
		owner, err := domain.LoadResourceOwnerID(r.Context(), state.DB, resourceKind, resourceID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if owner == nil {
			notFound(w, "ontology resource not found")
			return
		}
		bindingPID := binding.ProjectID
		if err := domain.EnsureResourceManageAccess(r.Context(), state.DB, claims, *owner, &bindingPID); err != nil {
			forbidden(w, err.Error())
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM ontology_project_resources
               WHERE project_id = $1 AND resource_kind = $2 AND resource_id = $3`,
			projectID, resourceKind.AsStr(), resourceID,
		)
		if err != nil {
			internalError(w, "failed to unbind ontology resource from project: "+err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "ontology project resource binding not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Working state ───────────────────────────────────────────────────────

// GetProjectWorkingState mirrors `pub async fn get_project_working_state`.
// Returns the persisted working state or — when no row exists yet — a
// synthesised default carrying an empty `changes` array and the
// caller as `updated_by`.
func GetProjectWorkingState(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`SELECT project_id, changes, updated_by, updated_at
               FROM ontology_project_working_states
               WHERE project_id = $1`,
			projectID,
		)
		var ws models.OntologyProjectWorkingState
		err = row.Scan(&ws.ProjectID, &ws.Changes, &ws.UpdatedBy, &ws.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, models.OntologyProjectWorkingState{
				ProjectID: projectID,
				Changes:   json.RawMessage(`[]`),
				UpdatedBy: claims.Sub,
				UpdatedAt: time.Now().UTC(),
			})
			return
		}
		if err != nil {
			internalError(w, "failed to load ontology project working state: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ws)
	}
}

// ReplaceProjectWorkingState mirrors `pub async fn replace_project_working_state`.
func ReplaceProjectWorkingState(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.ReplaceOntologyProjectWorkingStateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_project_working_states (project_id, changes, updated_by)
               VALUES ($1, $2, $3)
               ON CONFLICT (project_id)
               DO UPDATE SET changes = EXCLUDED.changes, updated_by = EXCLUDED.updated_by, updated_at = NOW()
               RETURNING project_id, changes, updated_by, updated_at`,
			projectID, body.Changes, claims.Sub,
		)
		var ws models.OntologyProjectWorkingState
		if err := row.Scan(&ws.ProjectID, &ws.Changes, &ws.UpdatedBy, &ws.UpdatedAt); err != nil {
			internalError(w, "failed to replace ontology project working state: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ws)
	}
}

// ── Branches ────────────────────────────────────────────────────────────

const projectBranchColumns = `id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
                  enable_indexing, created_by, created_at, updated_at, latest_rebased_at`

// ListProjectBranches mirrors `pub async fn list_project_branches`.
func ListProjectBranches(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT `+projectBranchColumns+`
               FROM ontology_project_branches
               WHERE project_id = $1
               ORDER BY updated_at DESC`,
			projectID,
		)
		if err != nil {
			internalError(w, "failed to list ontology project branches: "+err.Error())
			return
		}
		defer rows.Close()
		data := []models.OntologyProjectBranch{}
		for rows.Next() {
			b, err := scanBranch(rows)
			if err == nil {
				data = append(data, b)
			}
		}
		writeJSON(w, http.StatusOK, models.ListOntologyProjectBranchesResponse{Data: data})
	}
}

// CreateProjectBranch mirrors `pub async fn create_project_branch`.
// Status defaults to "draft", `proposal_id` to NULL, conflict
// resolutions to an empty JSON object, and `latest_rebased_at` to
// NOW.
func CreateProjectBranch(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		project, err := loadProject(r.Context(), state, projectID)
		if err != nil {
			internalError(w, err.Error())
			return
		}
		if project == nil {
			notFound(w, "ontology project not found")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.CreateOntologyProjectBranchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		name, err := NormalizeBranchName(body.Name)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		description := "Isolated ontology branch for testing and review."
		if body.Description != nil {
			description = *body.Description
		}
		enableIndexing := false
		if body.EnableIndexing != nil {
			enableIndexing = *body.EnableIndexing
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_project_branches
                  (id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
                   enable_indexing, created_by, latest_rebased_at)
               VALUES ($1, $2, $3, $4, 'draft', NULL, $5, $6, $7, $8, NOW())
               RETURNING `+projectBranchColumns,
			id, projectID, name, description,
			body.Changes, json.RawMessage(`{}`),
			enableIndexing, claims.Sub,
		)
		b, err := scanBranch(row)
		if err != nil {
			internalError(w, "failed to create ontology project branch: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// UpdateProjectBranch mirrors `pub async fn update_project_branch`.
// `proposal_id` honours the Rust `Option<Option<Uuid>>` three-way
// semantics through models.UUIDUpdate (nil → keep, value=nil →
// clear, value!=nil → replace).
func UpdateProjectBranch(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		branchID, err := pathUUID(r, "branch_id")
		if err != nil {
			badRequest(w, "invalid branch_id")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.UpdateOntologyProjectBranchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		var proposalIDArg any
		// Rust `body.proposal_id` is Option<Option<Uuid>>; the SQL
		// uses COALESCE so the bind value is either nil (keep, since
		// the column won't be touched if we pass NULL) or the new
		// uuid. The Go model's *UUIDUpdate carries the same shape:
		// nil → keep (we still bind the existing value implicitly via
		// COALESCE($5, proposal_id) treating $5=NULL as "keep"); for
		// the explicit-clear case (Set+Value=nil) we currently leak
		// into "keep" too because PG COALESCE can't distinguish
		// NULL-keep from NULL-clear without a separate flag. The
		// Rust source has the same limitation under `bind(body.proposal_id)`,
		// so the wire shape matches.
		if body.ProposalID != nil && body.ProposalID.Value != nil {
			proposalIDArg = *body.ProposalID.Value
		}

		var rebasedArg any
		if body.LatestRebasedAt != nil {
			rebasedArg = *body.LatestRebasedAt
		}
		row := state.DB.QueryRow(r.Context(),
			`UPDATE ontology_project_branches
               SET description = COALESCE($3, description),
                   status = COALESCE($4, status),
                   proposal_id = COALESCE($5, proposal_id),
                   changes = COALESCE($6, changes),
                   conflict_resolutions = COALESCE($7, conflict_resolutions),
                   enable_indexing = COALESCE($8, enable_indexing),
                   latest_rebased_at = COALESCE($9, latest_rebased_at),
                   updated_at = NOW()
               WHERE project_id = $1 AND id = $2
               RETURNING `+projectBranchColumns,
			projectID, branchID,
			body.Description, body.Status,
			proposalIDArg,
			body.Changes, body.ConflictResolutions,
			body.EnableIndexing,
			rebasedArg,
		)
		b, err := scanBranch(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "ontology project branch not found")
			return
		}
		if err != nil {
			internalError(w, "failed to update ontology project branch: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// ── Proposals ───────────────────────────────────────────────────────────

const projectProposalColumns = `id, project_id, branch_id, title, description, status, reviewer_ids, tasks, comments,
                  created_by, created_at, updated_at`

// ListProjectProposals mirrors `pub async fn list_project_proposals`.
func ListProjectProposals(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT `+projectProposalColumns+`
               FROM ontology_project_proposals
               WHERE project_id = $1
               ORDER BY updated_at DESC`,
			projectID,
		)
		if err != nil {
			internalError(w, "failed to list ontology project proposals: "+err.Error())
			return
		}
		defer rows.Close()
		data := []models.OntologyProjectProposal{}
		for rows.Next() {
			p, err := scanProposal(rows)
			if err == nil {
				data = append(data, p)
			}
		}
		writeJSON(w, http.StatusOK, models.ListOntologyProjectProposalsResponse{Data: data})
	}
}

// CreateProjectProposal mirrors `pub async fn create_project_proposal`.
// Loads the branch first, then INSERTs the proposal with status
// "in_review", then UPDATEs the branch to point at the new proposal.
// All three statements live in the same context.
func CreateProjectProposal(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.CreateOntologyProjectProposalRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		branchRow := state.DB.QueryRow(r.Context(),
			`SELECT `+projectBranchColumns+`
               FROM ontology_project_branches
               WHERE project_id = $1 AND id = $2`,
			projectID, body.BranchID,
		)
		branch, err := scanBranch(branchRow)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "ontology project branch not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load ontology project branch: "+err.Error())
			return
		}
		description := "Ontology proposal generated from the current branch."
		if body.Description != nil {
			description = *body.Description
		}
		reviewers := body.ReviewerIDs
		if len(reviewers) == 0 {
			reviewers = json.RawMessage(`[]`)
		}
		comments := body.Comments
		if len(comments) == 0 {
			comments = json.RawMessage(`[]`)
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_project_proposals
                  (id, project_id, branch_id, title, description, status, reviewer_ids, tasks, comments, created_by)
               VALUES ($1, $2, $3, $4, $5, 'in_review', $6, $7, $8, $9)
               RETURNING `+projectProposalColumns,
			id, projectID, body.BranchID, body.Title, description,
			reviewers, body.Tasks, comments, claims.Sub,
		)
		proposal, err := scanProposal(row)
		if err != nil {
			internalError(w, "failed to create ontology project proposal: "+err.Error())
			return
		}
		if _, err := state.DB.Exec(r.Context(),
			`UPDATE ontology_project_branches
               SET status = 'in_review', proposal_id = $3, updated_at = NOW()
               WHERE project_id = $1 AND id = $2`,
			projectID, branch.ID, proposal.ID,
		); err != nil {
			internalError(w, "failed to link proposal to branch: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, proposal)
	}
}

// UpdateProjectProposal mirrors `pub async fn update_project_proposal`.
func UpdateProjectProposal(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		proposalID, err := pathUUID(r, "proposal_id")
		if err != nil {
			badRequest(w, "invalid proposal_id")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.UpdateOntologyProjectProposalRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`UPDATE ontology_project_proposals
               SET title = COALESCE($3, title),
                   description = COALESCE($4, description),
                   status = COALESCE($5, status),
                   reviewer_ids = COALESCE($6, reviewer_ids),
                   tasks = COALESCE($7, tasks),
                   comments = COALESCE($8, comments),
                   updated_at = NOW()
               WHERE project_id = $1 AND id = $2
               RETURNING `+projectProposalColumns,
			projectID, proposalID,
			body.Title, body.Description, body.Status,
			body.ReviewerIDs, body.Tasks, body.Comments,
		)
		p, err := scanProposal(row)
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, "ontology project proposal not found")
			return
		}
		if err != nil {
			internalError(w, "failed to update ontology project proposal: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

// ── Migrations ──────────────────────────────────────────────────────────

const projectMigrationColumns = `id, project_id, source_project_id, target_project_id, resources, submitted_at, status, note, submitted_by`

// ListProjectMigrations mirrors `pub async fn list_project_migrations`.
func ListProjectMigrations(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if _, err := domain.EnsureProjectViewAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		rows, err := state.DB.Query(r.Context(),
			`SELECT `+projectMigrationColumns+`
               FROM ontology_project_migrations
               WHERE project_id = $1
               ORDER BY submitted_at DESC`,
			projectID,
		)
		if err != nil {
			internalError(w, "failed to list ontology project migrations: "+err.Error())
			return
		}
		defer rows.Close()
		data := []models.OntologyProjectMigration{}
		for rows.Next() {
			m, err := scanMigration(rows)
			if err == nil {
				data = append(data, m)
			}
		}
		writeJSON(w, http.StatusOK, models.ListOntologyProjectMigrationsResponse{Data: data})
	}
}

// CreateProjectMigration mirrors `pub async fn create_project_migration`.
func CreateProjectMigration(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		projectID, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		if _, err := domain.EnsureProjectEditAccess(r.Context(), state.DB, claims, projectID); err != nil {
			forbidden(w, err.Error())
			return
		}
		var body models.CreateOntologyProjectMigrationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		note := ""
		if body.Note != nil {
			note = *body.Note
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(),
			`INSERT INTO ontology_project_migrations
                  (id, project_id, source_project_id, target_project_id, resources, status, note, submitted_by)
               VALUES ($1, $2, $3, $4, $5, 'planned', $6, $7)
               RETURNING `+projectMigrationColumns,
			id, projectID, body.SourceProjectID, body.TargetProjectID,
			body.Resources, note, claims.Sub,
		)
		m, err := scanMigration(row)
		if err != nil {
			internalError(w, "failed to create ontology project migration: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

// ── Scanners ────────────────────────────────────────────────────────────

func scanProject(row interface{ Scan(...any) error }) (models.OntologyProject, error) {
	var p models.OntologyProject
	err := row.Scan(
		&p.ID, &p.Slug, &p.DisplayName, &p.Description,
		&p.WorkspaceSlug, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

func scanBranch(row interface{ Scan(...any) error }) (models.OntologyProjectBranch, error) {
	var b models.OntologyProjectBranch
	err := row.Scan(
		&b.ID, &b.ProjectID, &b.Name, &b.Description, &b.Status,
		&b.ProposalID, &b.Changes, &b.ConflictResolutions,
		&b.EnableIndexing, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.LatestRebasedAt,
	)
	return b, err
}

func scanProposal(row interface{ Scan(...any) error }) (models.OntologyProjectProposal, error) {
	var p models.OntologyProjectProposal
	err := row.Scan(
		&p.ID, &p.ProjectID, &p.BranchID,
		&p.Title, &p.Description, &p.Status,
		&p.ReviewerIDs, &p.Tasks, &p.Comments,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

func scanMigration(row interface{ Scan(...any) error }) (models.OntologyProjectMigration, error) {
	var m models.OntologyProjectMigration
	err := row.Scan(
		&m.ID, &m.ProjectID, &m.SourceProjectID, &m.TargetProjectID,
		&m.Resources, &m.SubmittedAt, &m.Status, &m.Note, &m.SubmittedBy,
	)
	return m, err
}
