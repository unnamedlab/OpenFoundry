package handlers

// projects.go exposes the ontology-project CRUD surface plus folder
// management, membership upserts and resource-binding lifecycle.
// Slug normalisation, folder-name canonicalisation and error strings
// are wire-stable so federated callers receive identical payloads.
//
// The handler holds a *pgxpool.Pool directly (mirroring the Rust
// `state.ontology_db` field) instead of going through the foundation
// repo.Repo — this matches Rust's inline-SQL style and keeps the SQL
// adjacency to the handler logic.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/libs/core-models/rid"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

// ProjectsHandlers owns the ontology-project HTTP surface. The Pool
// is the same shared pgxpool used by the rest of the service:
// projects run against the single DATABASE_URL alongside organizations.
type ProjectsHandlers struct {
	Pool *pgxpool.Pool
}

const folderSelectColumns = `f.id, f.rid, f.project_id, f.parent_folder_id,
        COALESCE(parent.rid, 'ri.compass.main.project.' || f.project_id::text),
        COALESCE(p.space_rid, 'ri.compass.main.folder.default-space'),
        f.name, f.slug, f.description, f.created_by, f.is_deleted,
        COALESCE(p.resource_level_role_grants_allowed, TRUE),
        COALESCE(f.propagate_view_requirements_enabled, FALSE),
        f.propagate_view_requirements_disabled_at,
        COALESCE(f.view_requirement_marking_rids, '[]'::jsonb),
        f.created_at, f.updated_at`

// ─── slug + folder-name normalisation ───────────────────────────────────

// asciiLower returns s with every ASCII A–Z byte lowered to a–z.
// Non-ASCII bytes pass through unchanged so we mirror Rust's
// str::to_ascii_lowercase byte-exactly.
func asciiLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// normalizeSlug enforces the Rust slug invariants verbatim:
// trim → ASCII lower-case → non-empty → only [a-z0-9-] →
// no leading or trailing hyphen.
func normalizeSlug(value, fieldName string) (string, error) {
	normalized := asciiLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	for _, ch := range normalized {
		ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-'
		if !ok {
			return "", fmt.Errorf("%s must contain only lowercase letters, digits, and hyphens", fieldName)
		}
	}
	if strings.HasPrefix(normalized, "-") || strings.HasSuffix(normalized, "-") {
		return "", fmt.Errorf("%s cannot start or end with a hyphen", fieldName)
	}
	return normalized, nil
}

// normalizeOptionalSlug mirrors normalize_optional_slug: a nil or
// whitespace-only input collapses to nil; otherwise the value is run
// through normalizeSlug.
func normalizeOptionalSlug(value *string, fieldName string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	out, err := normalizeSlug(trimmed, fieldName)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// normalizeFolderName collapses Unicode whitespace runs to a single
// space and rejects whitespace-only input. Mirrors Rust's
// `value.split_whitespace().collect::<Vec<_>>().join(" ")`.
func normalizeFolderName(value string) (string, error) {
	parts := strings.Fields(value)
	normalized := strings.Join(parts, " ")
	if normalized == "" {
		return "", errors.New("Folder name is required.")
	}
	return normalized, nil
}

// folderSlugFromName ports the Rust slug derivation: walk runes, lower
// ASCII letters, keep ASCII alphanumerics, collapse every other run of
// characters into a single hyphen, strip trailing hyphens, error on an
// empty result.
func folderSlugFromName(value string) (string, error) {
	var slug strings.Builder
	slug.Grow(len(value))
	lastWasHyphen := false
	for _, ch := range strings.TrimSpace(value) {
		normalized := ch
		if 'A' <= ch && ch <= 'Z' {
			normalized = ch + ('a' - 'A')
		}
		if isASCIIAlphaNum(normalized) {
			slug.WriteRune(normalized)
			lastWasHyphen = false
		} else if !lastWasHyphen && slug.Len() > 0 {
			slug.WriteByte('-')
			lastWasHyphen = true
		}
	}
	out := strings.TrimRight(slug.String(), "-")
	if out == "" {
		return "", errors.New("folder name must contain letters or numbers")
	}
	return out, nil
}

func isASCIIAlphaNum(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

// ─── helpers ────────────────────────────────────────────────────────────

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err == nil {
		return id, true
	}
	parsedRID, ridErr := rid.ParseUUID(raw)
	if ridErr == nil {
		if parsed, ok := parsedRID.UUID(); ok {
			return parsed, true
		}
	}
	writeJSONErr(w, http.StatusBadRequest, "invalid "+label)
	return uuid.Nil, false
}

func authClaims(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	user, ok := authmw.AuthUserFromRequest(r)
	if !ok || user.Claims == nil {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return user.Claims, true
}

func loadProject(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*models.OntologyProject, error) {
	row := pool.QueryRow(ctx,
		`SELECT id, COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        slug, display_name, description, workspace_slug, owner_id,
		        default_role, point_of_contact_user_id, point_of_contact_email,
		        "references", COALESCE(marking_rids, '[]'::jsonb),
		        COALESCE(propagate_view_requirements_enabled, FALSE),
		        propagate_view_requirements_disabled_at,
		        created_at, updated_at
		 FROM ontology_projects
		 WHERE id = $1 AND is_deleted = FALSE`,
		id,
	)
	return scanProjectRow(row)
}

// projectScannable is the minimal Scan(...any) contract shared by
// pgx.Row (single-row QueryRow result) and pgx.Rows (streaming
// query result). scanProjectRow accepts either so the helper can be
// reused by loadProject (single) and ListProjects (rows).
type projectScannable interface {
	Scan(dest ...any) error
}

type folderQueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// scanProjectRow is the shared scan path used by loadProject and any
// future list/get helper that wants the SG.6-extended shape.
func scanProjectRow(row projectScannable) (*models.OntologyProject, error) {
	p := &models.OntologyProject{}
	var defaultRole string
	var refs []byte
	var markings []byte
	err := row.Scan(
		&p.ID, &p.RID, &p.Slug, &p.DisplayName, &p.Description,
		&p.WorkspaceSlug, &p.OwnerID,
		&defaultRole, &p.PointOfContactUserID, &p.PointOfContactEmail,
		&refs, &markings, &p.PropagateViewRequirementsEnabled,
		&p.PropagateViewRequirementsDisabledAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load ontology project: %s", err)
	}
	p.DefaultRole = models.OntologyProjectRole(defaultRole)
	if len(refs) == 0 {
		p.References = []models.OntologyProjectReference{}
	} else if err := json.Unmarshal(refs, &p.References); err != nil {
		return nil, fmt.Errorf("decode project references: %s", err)
	}
	if p.References == nil {
		p.References = []models.OntologyProjectReference{}
	}
	p.MarkingRIDs = decodeStringSliceJSON(markings)
	if p.MarkingRIDs == nil {
		p.MarkingRIDs = []string{}
	}
	return p, nil
}

func loadProjectFolder(ctx context.Context, q folderQueryRower, projectID, folderID uuid.UUID) (*models.OntologyProjectFolder, error) {
	row := q.QueryRow(ctx,
		`SELECT `+folderSelectColumns+`
		   FROM ontology_project_folders f
		   JOIN ontology_projects p ON p.id = f.project_id
		   LEFT JOIN ontology_project_folders parent ON parent.id = f.parent_folder_id
		  WHERE f.project_id = $1 AND f.id = $2 AND f.is_deleted = FALSE`,
		projectID, folderID,
	)
	return scanProjectFolderRow(row)
}

func scanProjectFolderRow(row projectScannable) (*models.OntologyProjectFolder, error) {
	f := &models.OntologyProjectFolder{}
	var isDeleted bool
	var viewRequirementMarkings []byte
	err := row.Scan(
		&f.ID, &f.RID, &f.ProjectID, &f.ParentFolderID,
		&f.ParentFolderRID, &f.SpaceRID,
		&f.Name, &f.Slug, &f.Description, &f.CreatedBy, &isDeleted,
		&f.PolicyOverridesAllowed, &f.PropagateViewRequirementsEnabled,
		&f.PropagateViewRequirementsDisabledAt, &viewRequirementMarkings,
		&f.CreatedAt, &f.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load ontology project folder: %s", err)
	}
	if strings.TrimSpace(f.RID) == "" {
		f.RID = models.FolderRIDFromID(f.ID)
	}
	f.ProjectRID = models.ProjectRIDFromID(f.ProjectID)
	if strings.TrimSpace(f.SpaceRID) == "" {
		f.SpaceRID = models.DefaultProjectSpaceRID
	}
	if strings.TrimSpace(f.ParentFolderRID) == "" {
		f.ParentFolderRID = f.ProjectRID
	}
	f.Type = models.FolderResourceType
	f.TrashStatus = models.FolderTrashStatusNotTrashed
	if isDeleted {
		f.TrashStatus = models.FolderTrashStatusDirectTrash
	}
	f.InheritsProjectPolicies = true
	f.ViewRequirementMarkingRIDs = decodeStringSliceJSON(viewRequirementMarkings)
	if f.ViewRequirementMarkingRIDs == nil {
		f.ViewRequirementMarkingRIDs = []string{}
	}
	return f, nil
}

func parseFolderRIDLocator(value, field string) (uuid.UUID, error) {
	parsed, err := rid.ParseUUID(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid folder RID: %s", field, err)
	}
	if parsed.Service != "compass" || parsed.ResourceType != "folder" {
		return uuid.Nil, fmt.Errorf("%s must be a compass folder RID", field)
	}
	id, ok := parsed.UUID()
	if !ok {
		return uuid.Nil, fmt.Errorf("%s must carry a UUID locator", field)
	}
	return id, nil
}

func resolveProjectFolderParent(
	ctx context.Context,
	q folderQueryRower,
	projectID uuid.UUID,
	projectRID string,
	folder *models.CreateOntologyProjectFolderRequest,
) (*uuid.UUID, int, string, error) {
	var parentID *uuid.UUID
	if folder.ParentFolderID != nil {
		parentID = folder.ParentFolderID
	}
	if folder.ParentFolderRID != nil {
		parentRID := strings.TrimSpace(*folder.ParentFolderRID)
		if parentRID == "" {
			return nil, http.StatusBadRequest, "parent_folder_rid must be a non-empty RID", nil
		}
		if parentRID == projectRID {
			if parentID != nil {
				return nil, http.StatusBadRequest, "parent_folder_id must be omitted when parent_folder_rid is the project RID", nil
			}
			return nil, 0, "", nil
		}
		id, err := parseFolderRIDLocator(parentRID, "parent_folder_rid")
		if err != nil {
			return nil, http.StatusBadRequest, err.Error(), nil
		}
		if parentID != nil && *parentID != id {
			return nil, http.StatusBadRequest, "parent_folder_id and parent_folder_rid refer to different folders", nil
		}
		parentID = &id
	}
	if parentID == nil {
		return nil, 0, "", nil
	}
	parent, err := loadProjectFolder(ctx, q, projectID, *parentID)
	if err != nil {
		return nil, 0, "", err
	}
	if parent == nil {
		return nil, http.StatusNotFound, "ontology project parent folder not found", nil
	}
	out := *parentID
	return &out, 0, "", nil
}

func insertProjectFolder(
	ctx context.Context,
	tx pgx.Tx,
	projectID, createdBy uuid.UUID,
	folder *models.CreateOntologyProjectFolderRequest,
	parentFolderID *uuid.UUID,
) (*models.OntologyProjectFolder, error) {
	name, err := normalizeFolderName(folder.Name)
	if err != nil {
		return nil, err
	}
	slug, err := folderSlugFromName(name)
	if err != nil {
		return nil, err
	}
	description := ""
	if folder.Description != nil {
		description = *folder.Description
	}
	folderID := ids.New()
	folderRID := models.FolderRIDFromID(folderID)
	inheritedMarkings, err := inheritedViewRequirementMarkings(ctx, tx, projectID, parentFolderID)
	if err != nil {
		return nil, fmt.Errorf("load inherited view requirements: %w", err)
	}
	viewRequirementMarkings := inheritedMarkings
	if folder.ViewRequirementMarkingRIDs != nil {
		viewRequirementMarkings = folder.ViewRequirementMarkingRIDs
	}
	viewRequirementMarkingsJSON, err := jsonStringSlice(viewRequirementMarkings)
	if err != nil {
		return nil, fmt.Errorf("encode view requirement markings: %w", err)
	}
	propagateViewRequirements := false
	if folder.PropagateViewRequirementsEnabled != nil {
		propagateViewRequirements = *folder.PropagateViewRequirementsEnabled
	}
	row := tx.QueryRow(ctx,
		`WITH inserted AS (
		     INSERT INTO ontology_project_folders (
		         id, rid, project_id, parent_folder_id, name, slug, description, created_by,
		         propagate_view_requirements_enabled, view_requirement_marking_rids
		     )
		     VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
		     RETURNING *
		 )
		 SELECT `+folderSelectColumns+`
		   FROM inserted f
		   JOIN ontology_projects p ON p.id = f.project_id
		   LEFT JOIN ontology_project_folders parent ON parent.id = f.parent_folder_id`,
		folderID, folderRID, projectID, parentFolderID, name, slug, description, createdBy,
		propagateViewRequirements, viewRequirementMarkingsJSON,
	)
	return scanProjectFolderRow(row)
}

func ensureProjectOwnerOrAdmin(project *models.OntologyProject, claims *authmw.Claims) error {
	if claims.HasRole("admin") || project.OwnerID == claims.Sub {
		return nil
	}
	return errors.New("forbidden: only the ontology project owner can manage memberships or delete the project")
}

// ─── list / create / get / update / delete ─────────────────────────────

// ListTemplates returns the active Project templates available for the
// create-project wizard. SG.26 makes this a persisted per-space surface;
// the optional space_slug query keeps the default global template visible
// while filtering space-scoped governance templates.
func (h *ProjectsHandlers) ListTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := authClaims(w, r); !ok {
		return
	}
	var spaceSlug *string
	if raw := strings.TrimSpace(r.URL.Query().Get("space_slug")); raw != "" {
		normalized, err := normalizeOptionalSlug(&raw, "space_slug")
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		spaceSlug = normalized
	}
	templates, err := listProjectTemplates(r.Context(), h.Pool, spaceSlug)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list project templates: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, models.ListProjectTemplatesResponse{Data: templates})
}

// ListProjects mirrors Rust `list_projects`: filter by search, paginate
// by (page, per_page), then trim to the visible set unless the caller
// is an admin.
func (h *ProjectsHandlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	accessible, err := domain.ListAccessibleProjects(r.Context(), h.Pool, claims)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to evaluate project access: %s", err))
		return
	}

	page := int64(1)
	perPage := int64(20)
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			perPage = n
		}
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 100 {
		perPage = 100
	}
	search := r.URL.Query().Get("search")
	pattern := "%" + search + "%"

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        slug, display_name, description, workspace_slug, owner_id,
		        default_role, point_of_contact_user_id, point_of_contact_email,
		        "references", COALESCE(marking_rids, '[]'::jsonb),
		        COALESCE(propagate_view_requirements_enabled, FALSE),
		        propagate_view_requirements_disabled_at,
		        created_at, updated_at
		 FROM ontology_projects
		 WHERE is_deleted = FALSE
		   AND (slug ILIKE $1 OR display_name ILIKE $1)
		 ORDER BY created_at DESC`,
		pattern,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology projects: %s", err))
		return
	}
	defer rows.Close()

	projects := make([]models.OntologyProject, 0)
	for rows.Next() {
		p, scanErr := scanProjectRow(rows)
		if scanErr != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology projects: %s", scanErr))
			return
		}
		if p == nil {
			continue
		}
		projects = append(projects, *p)
	}
	if err := rows.Err(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology projects: %s", err))
		return
	}

	visible := projects
	if !claims.HasRole("admin") {
		visible = visible[:0]
		for _, p := range projects {
			if _, allowed := accessible[p.ID]; allowed {
				visible = append(visible, p)
			}
		}
	}

	total := int64(len(visible))
	offset := (page - 1) * perPage
	if offset > int64(len(visible)) {
		offset = int64(len(visible))
	}
	end := offset + perPage
	if end > int64(len(visible)) {
		end = int64(len(visible))
	}
	data := append([]models.OntologyProject{}, visible[offset:end]...)

	writeJSON(w, http.StatusOK, models.ListOntologyProjectsResponse{
		Data:    data,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// CreateProject mirrors Rust `create_project`. Folder creation is part
// of the same transaction so an inserted project always sees its
// declared folders or nothing at all.
func (h *ProjectsHandlers) CreateProject(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	var body models.CreateOntologyProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	slug, err := normalizeSlug(body.Slug, "slug")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	displayName := slug
	if body.DisplayName != nil {
		displayName = *body.DisplayName
	}
	deployment, err := h.prepareProjectTemplateDeployment(r.Context(), claims, &body, slug, displayName)
	if err != nil {
		var typed projectTemplateHTTPError
		if errors.As(err, &typed) {
			writeJSONErr(w, typed.status, typed.message)
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	workspaceSlug, err := normalizeOptionalSlug(body.WorkspaceSlug, "workspace_slug")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}

	// SG.6: optional default_role / contact / references on create.
	defaultRole := models.OntologyProjectRoleViewer
	if body.DefaultRole != nil {
		parsed, parseErr := models.ParseOntologyProjectRole(string(*body.DefaultRole))
		if parseErr != nil {
			writeJSONErr(w, http.StatusBadRequest, parseErr.Error())
			return
		}
		defaultRole = parsed
	}
	if body.References == nil {
		body.References = []models.OntologyProjectReference{}
	}
	refsJSON, err := json.Marshal(body.References)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("encode references: %s", err))
		return
	}
	markingRIDs := normalizeStringValues(body.MarkingRIDs)
	if len(markingRIDs) > 0 && !canApplyProjectCreationMarkings(claims, markingRIDs) {
		writeJSONErr(w, http.StatusForbidden, "missing permission markings:apply for file access preset markings")
		return
	}
	markingRIDsJSON, err := json.Marshal(markingRIDs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("encode marking_rids: %s", err))
		return
	}
	propagateViewRequirements := false
	if body.PropagateViewRequirementsEnabled != nil {
		propagateViewRequirements = *body.PropagateViewRequirementsEnabled
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start ontology project transaction: %s", err))
		return
	}
	defer tx.Rollback(context.Background())

	projectID := ids.New()
	projectRID := models.ProjectRIDFromID(projectID)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ontology_projects
		   (id, rid, slug, display_name, description, workspace_slug, owner_id,
		    default_role, point_of_contact_user_id, point_of_contact_email, "references", marking_rids,
		    propagate_view_requirements_enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, $13)`,
		projectID, projectRID, slug, displayName, description, workspaceSlug, claims.Sub,
		string(defaultRole), body.PointOfContactUserID, body.PointOfContactEmail, refsJSON, markingRIDsJSON,
		propagateViewRequirements,
	); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to create ontology project: %s", err))
		return
	}

	for i := range body.Folders {
		parentFolderID, status, msg, err := resolveProjectFolderParent(r.Context(), tx, projectID, projectRID, &body.Folders[i])
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if status != 0 {
			writeJSONErr(w, status, msg)
			return
		}
		folder, err := insertProjectFolder(r.Context(), tx, projectID, claims.Sub, &body.Folders[i], parentFolderID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := workspace.UpsertFolderSearchIndexTx(r.Context(), tx, folder.ID, workspace.ResourceSearchEventCreated); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index ontology project folder: %s", err))
			return
		}
	}
	if deployment != nil {
		if err := deployment.insertFolders(r.Context(), tx, projectID, projectRID, claims.Sub); err != nil {
			var typed projectTemplateHTTPError
			if errors.As(err, &typed) {
				writeJSONErr(w, typed.status, typed.message)
				return
			}
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := deployment.applyPostCreate(r.Context(), tx, projectID, slug, claims.Sub); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if len(markingRIDs) > 0 {
		if err := mergeProjectMarkingRIDs(r.Context(), tx, projectID, markingRIDs); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := workspace.UpsertProjectSearchIndexTx(r.Context(), tx, projectID, workspace.ResourceSearchEventCreated); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index ontology project: %s", err))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit ontology project transaction: %s", err))
		return
	}
	project, err := loadProject(r.Context(), h.Pool, projectID)
	if err != nil || project == nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to reload created project")
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func mergeProjectMarkingRIDs(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, markingRIDs []string) error {
	markingRIDs = normalizeStringValues(markingRIDs)
	if len(markingRIDs) == 0 {
		return nil
	}
	var raw []byte
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(marking_rids, '[]'::jsonb)
		   FROM ontology_projects
		  WHERE id = $1`,
		projectID,
	).Scan(&raw); err != nil {
		return fmt.Errorf("load project markings: %w", err)
	}
	existing := []string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("decode project markings: %w", err)
		}
	}
	merged, err := json.Marshal(normalizeStringValues(append(existing, markingRIDs...)))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ontology_projects
		    SET marking_rids = $2::jsonb, updated_at = NOW()
		  WHERE id = $1`,
		projectID, merged,
	); err != nil {
		return fmt.Errorf("merge project markings: %w", err)
	}
	return nil
}

func decodeStringSliceJSON(raw []byte) []string {
	values := []string{}
	if len(raw) == 0 {
		return values
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return normalizeStringValues(values)
}

func jsonStringSlice(values []string) ([]byte, error) {
	normalized := normalizeStringValues(values)
	if normalized == nil {
		normalized = []string{}
	}
	return json.Marshal(normalized)
}

func resolveProjectPropagationPatch(enabled bool, disabledAt *time.Time, requested bool, explicit bool, now time.Time) (bool, *time.Time, int, string) {
	if !explicit {
		return enabled, disabledAt, 0, ""
	}
	if requested {
		if disabledAt != nil {
			return enabled, disabledAt, http.StatusConflict, "propagate view requirements cannot be re-enabled after it has been disabled; migrate to Markings instead"
		}
		return true, nil, 0, ""
	}
	if enabled && disabledAt == nil {
		disabled := now.UTC()
		return false, &disabled, 0, ""
	}
	return false, disabledAt, 0, ""
}

func loadProjectPropagationSource(ctx context.Context, q folderQueryRower, projectID uuid.UUID) (bool, []string, error) {
	var enabled bool
	var markings []byte
	err := q.QueryRow(ctx,
		`SELECT COALESCE(propagate_view_requirements_enabled, FALSE),
		        COALESCE(marking_rids, '[]'::jsonb)
		   FROM ontology_projects
		  WHERE id = $1 AND is_deleted = FALSE`,
		projectID,
	).Scan(&enabled, &markings)
	if err != nil {
		return false, nil, err
	}
	return enabled, decodeStringSliceJSON(markings), nil
}

func inheritedViewRequirementMarkings(
	ctx context.Context,
	q folderQueryRower,
	projectID uuid.UUID,
	parentFolderID *uuid.UUID,
) ([]string, error) {
	if parentFolderID != nil {
		var enabled bool
		var markings []byte
		err := q.QueryRow(ctx,
			`SELECT COALESCE(propagate_view_requirements_enabled, FALSE),
			        COALESCE(view_requirement_marking_rids, '[]'::jsonb)
			   FROM ontology_project_folders
			  WHERE project_id = $1 AND id = $2 AND is_deleted = FALSE`,
			projectID, *parentFolderID,
		).Scan(&enabled, &markings)
		if err != nil {
			return nil, err
		}
		if enabled {
			return decodeStringSliceJSON(markings), nil
		}
	}
	enabled, markings, err := loadProjectPropagationSource(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return []string{}, nil
	}
	return markings, nil
}

func canApplyProjectCreationMarkings(claims *authmw.Claims, markingRIDs []string) bool {
	if len(markingRIDs) == 0 {
		return true
	}
	if hasAnyPermissionKey(claims, "markings:apply", "markings:write", "markings:manage") {
		return true
	}
	allowed := projectCreationApplyMarkingIDsFromClaims(claims)
	for _, markingRID := range markingRIDs {
		if !containsStringFold(allowed, markingRID) {
			return false
		}
	}
	return true
}

func projectCreationApplyMarkingIDsFromClaims(claims *authmw.Claims) []string {
	if claims == nil {
		return []string{}
	}
	values := []string{}
	for _, permission := range claims.Permissions {
		permission = strings.TrimSpace(permission)
		parts := strings.Split(permission, ":")
		if len(parts) == 3 && (parts[0] == "marking" || parts[0] == "markings") && parts[2] == "apply" {
			values = append(values, parts[1])
			continue
		}
		if strings.HasPrefix(permission, "markings:apply:") {
			values = append(values, strings.TrimPrefix(permission, "markings:apply:"))
		}
	}
	if len(claims.Attributes) > 0 {
		var attrs map[string]any
		if err := json.Unmarshal(claims.Attributes, &attrs); err == nil {
			for _, key := range []string{"apply_marking_ids", "marking_apply_ids", "appliable_marking_ids", "allowed_apply_marking_ids"} {
				values = appendProjectCreationAttributeStrings(values, attrs[key])
			}
		}
	}
	return normalizeStringValues(values)
}

func appendProjectCreationAttributeStrings(values []string, raw any) []string {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case []string:
		values = append(values, v...)
	case string:
		values = append(values, v)
	}
	return values
}

func containsStringFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}

// GetProject mirrors Rust `get_project`.
func (h *ProjectsHandlers) GetProject(w http.ResponseWriter, r *http.Request) {
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
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, id); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, project)
}

// UpdateProject mirrors Rust `update_project`. The `workspace_slug`
// field follows the Rust `Option<Option<String>>` triple-state:
// absent → keep current, explicit null → clear, present string →
// normalise. Detection requires re-decoding the body as
// map[string]json.RawMessage because Go's typed `*string` cannot
// distinguish absent from explicit null on its own.
func (h *ProjectsHandlers) UpdateProject(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	existing, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := ensureProjectOwnerOrAdmin(existing, claims); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	raw, err := readJSONBody(r)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	displayName, err := optionalStringField(raw, "display_name")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	description, err := optionalStringField(raw, "description")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}

	workspaceSlug := existing.WorkspaceSlug
	if rawWS, present := raw["workspace_slug"]; present {
		trimmed := strings.TrimSpace(string(rawWS))
		switch {
		case trimmed == "null":
			workspaceSlug = nil
		default:
			var s string
			if err := json.Unmarshal(rawWS, &s); err != nil {
				writeJSONErr(w, http.StatusBadRequest, "workspace_slug must be a string")
				return
			}
			normalized, err := normalizeOptionalSlug(&s, "workspace_slug")
			if err != nil {
				writeJSONErr(w, http.StatusBadRequest, err.Error())
				return
			}
			workspaceSlug = normalized
		}
	}

	// SG.6: optional patches for default_role / contact / references.
	defaultRole := existing.DefaultRole
	if rawRole, present := raw["default_role"]; present {
		var s string
		if err := json.Unmarshal(rawRole, &s); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "default_role must be a string")
			return
		}
		parsed, parseErr := models.ParseOntologyProjectRole(s)
		if parseErr != nil {
			writeJSONErr(w, http.StatusBadRequest, parseErr.Error())
			return
		}
		defaultRole = parsed
	}
	pocUserID := existing.PointOfContactUserID
	if rawPOC, present := raw["point_of_contact_user_id"]; present {
		trimmed := strings.TrimSpace(string(rawPOC))
		if trimmed == "null" {
			pocUserID = nil
		} else {
			var parsed uuid.UUID
			if err := json.Unmarshal(rawPOC, &parsed); err != nil {
				writeJSONErr(w, http.StatusBadRequest, "point_of_contact_user_id must be a uuid or null")
				return
			}
			pocUserID = &parsed
		}
	}
	pocEmail := existing.PointOfContactEmail
	if rawEmail, present := raw["point_of_contact_email"]; present {
		trimmed := strings.TrimSpace(string(rawEmail))
		if trimmed == "null" {
			pocEmail = nil
		} else {
			var parsed string
			if err := json.Unmarshal(rawEmail, &parsed); err != nil {
				writeJSONErr(w, http.StatusBadRequest, "point_of_contact_email must be a string or null")
				return
			}
			pocEmail = &parsed
		}
	}
	refs := existing.References
	if rawRefs, present := raw["references"]; present {
		var parsed []models.OntologyProjectReference
		if err := json.Unmarshal(rawRefs, &parsed); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "references must be an array of {kind,id} objects")
			return
		}
		refs = parsed
	}
	if refs == nil {
		refs = []models.OntologyProjectReference{}
	}
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("encode references: %s", err))
		return
	}
	propagateViewRequirements := existing.PropagateViewRequirementsEnabled
	propagateDisabledAt := existing.PropagateViewRequirementsDisabledAt
	propagationPolicyTouched := false
	if rawPropagate, present := raw["propagate_view_requirements_enabled"]; present {
		propagationPolicyTouched = true
		var requested bool
		if err := json.Unmarshal(rawPropagate, &requested); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "propagate_view_requirements_enabled must be a boolean")
			return
		}
		nextEnabled, nextDisabledAt, status, message := resolveProjectPropagationPatch(
			existing.PropagateViewRequirementsEnabled,
			existing.PropagateViewRequirementsDisabledAt,
			requested,
			true,
			time.Now(),
		)
		if status != 0 {
			writeJSONErr(w, status, message)
			return
		}
		propagateViewRequirements = nextEnabled
		propagateDisabledAt = nextDisabledAt
	}
	previousPropagationMarkings := []string{}
	if existing.PropagateViewRequirementsEnabled {
		previousPropagationMarkings = existing.MarkingRIDs
	}
	nextPropagationMarkings := []string{}
	if propagateViewRequirements {
		nextPropagationMarkings = existing.MarkingRIDs
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start ontology project transaction: %s", err))
		return
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(r.Context(),
		`UPDATE ontology_projects
		 SET display_name = COALESCE($2, display_name),
		     description = COALESCE($3, description),
		     workspace_slug = $4,
		     default_role = $5,
		     point_of_contact_user_id = $6,
		     point_of_contact_email = $7,
		     "references" = $8::jsonb,
		     propagate_view_requirements_enabled = $9,
		     propagate_view_requirements_disabled_at = $10,
		     updated_at = NOW()
		 WHERE id = $1`,
		id, displayName, description, workspaceSlug,
		string(defaultRole), pocUserID, pocEmail, refsJSON,
		propagateViewRequirements, propagateDisabledAt,
	); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to update ontology project: %s", err))
		return
	}
	if err := workspace.UpsertProjectSearchIndexTx(r.Context(), tx, id, workspace.ResourceSearchEventUpdated); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index ontology project: %s", err))
		return
	}
	var propagationJob *models.ViewRequirementPropagationJob
	if propagationPolicyTouched && !sameStringSlice(previousPropagationMarkings, nextPropagationMarkings) {
		propagationJob, err = insertViewRequirementPropagationJobTx(
			r.Context(),
			tx,
			id,
			viewReqParentProject,
			id,
			existing.RID,
			claims.Sub,
			nextPropagationMarkings,
			previousPropagationMarkings,
		)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to enqueue view requirement propagation job: %s", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit ontology project transaction: %s", err))
		return
	}
	h.launchViewRequirementPropagationJob(propagationJob)
	updated, err := loadProject(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DeleteProject moves the project into Trash. Permanent deletion is handled by
// DELETE /api/v1/workspace/resources/ontology_project/{id}/purge.
func (h *ProjectsHandlers) DeleteProject(w http.ResponseWriter, r *http.Request) {
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

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start ontology project transaction: %s", err))
		return
	}
	defer tx.Rollback(context.Background())
	cmd, err := tx.Exec(r.Context(),
		`UPDATE ontology_projects
		    SET is_deleted = TRUE,
		        deleted_at = NOW(),
		        deleted_by = $2,
		        trash_retention_days = $3,
		        purge_after = NOW() + ($3::int * INTERVAL '1 day'),
		        original_project_id = NULL,
		        original_parent_folder_id = NULL,
		        updated_at = NOW()
		  WHERE id = $1 AND is_deleted = FALSE`,
		id, claims.Sub, workspace.DefaultTrashRetentionDays)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to move ontology project to trash: %s", err))
		return
	}
	if cmd.RowsAffected() == 0 {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := workspace.UpsertProjectSearchIndexTx(r.Context(), tx, id, workspace.ResourceSearchEventTrashed); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index trashed ontology project: %s", err))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit ontology project transaction: %s", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── memberships ───────────────────────────────────────────────────────

// ListProjectMemberships mirrors Rust `list_project_memberships`.
func (h *ProjectsHandlers) ListProjectMemberships(w http.ResponseWriter, r *http.Request) {
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
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, id); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT project_id, user_id, role, created_at, updated_at
		 FROM ontology_project_memberships
		 WHERE project_id = $1
		 ORDER BY created_at ASC`,
		id,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project memberships: %s", err))
		return
	}
	defer rows.Close()

	out := make([]models.OntologyProjectMembership, 0)
	for rows.Next() {
		var m models.OntologyProjectMembership
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project memberships: %s", err))
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project memberships: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, models.ListOntologyProjectMembershipsResponse{Data: out})
}

// UpsertProjectMembership mirrors Rust `upsert_project_membership`.
// Idempotent: a repeat upsert with the same (project, user, role) is a
// no-op; an upsert with a new role updates the row in place.
func (h *ProjectsHandlers) UpsertProjectMembership(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	var body models.UpsertOntologyProjectMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	role, err := models.ParseOntologyProjectRole(string(body.Role))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
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

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ontology_project_memberships (project_id, user_id, role)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id)
		 DO UPDATE SET role = EXCLUDED.role, updated_at = NOW()
		 RETURNING project_id, user_id, role, created_at, updated_at`,
		id, body.UserID, string(role),
	)
	out := &models.OntologyProjectMembership{}
	if err := row.Scan(&out.ProjectID, &out.UserID, &out.Role, &out.CreatedAt, &out.UpdatedAt); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to upsert ontology project membership: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteProjectMembership mirrors Rust `delete_project_membership`.
func (h *ProjectsHandlers) DeleteProjectMembership(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	userID, ok := parseUUIDParam(w, r, "user_id", "user_id")
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

	cmd, err := h.Pool.Exec(r.Context(),
		`DELETE FROM ontology_project_memberships
		 WHERE project_id = $1 AND user_id = $2`,
		projectID, userID,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete ontology project membership: %s", err))
		return
	}
	if cmd.RowsAffected() == 0 {
		writeJSONErr(w, http.StatusNotFound, "ontology project membership not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── folders ───────────────────────────────────────────────────────────

// ListProjectFolders mirrors Rust `list_project_folders`.
func (h *ProjectsHandlers) ListProjectFolders(w http.ResponseWriter, r *http.Request) {
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
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, id); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+folderSelectColumns+`
		   FROM ontology_project_folders f
		   JOIN ontology_projects p ON p.id = f.project_id
		   LEFT JOIN ontology_project_folders parent ON parent.id = f.parent_folder_id
		  WHERE f.project_id = $1 AND f.is_deleted = FALSE
		  ORDER BY f.created_at ASC`,
		id,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project folders: %s", err))
		return
	}
	defer rows.Close()

	out := make([]models.OntologyProjectFolder, 0)
	for rows.Next() {
		f, err := scanProjectFolderRow(rows)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project folders: %s", err))
			return
		}
		out = append(out, *f)
	}
	if err := rows.Err(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project folders: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, models.ListOntologyProjectFoldersResponse{Data: out})
}

// CreateProjectFolder mirrors Rust `create_project_folder`. Parent
// folder presence is validated before insert so the FK can stay
// `ON DELETE SET NULL` without surprising callers.
func (h *ProjectsHandlers) CreateProjectFolder(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	var body models.CreateOntologyProjectFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
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
	if _, err := domain.EnsureProjectEditAccess(r.Context(), h.Pool, claims, projectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	parentFolderID, status, msg, err := resolveProjectFolderParent(r.Context(), h.Pool, projectID, models.ProjectRIDFromID(projectID), &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != 0 {
		writeJSONErr(w, status, msg)
		return
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start ontology folder transaction: %s", err))
		return
	}
	defer tx.Rollback(context.Background())

	folder, err := insertProjectFolder(r.Context(), tx, projectID, claims.Sub, &body, parentFolderID)
	if err != nil {
		// normalize_folder_name / folder_slug_from_name surface as 500 in
		// the Rust source (db_error). We mirror that to keep parity.
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := workspace.UpsertFolderSearchIndexTx(r.Context(), tx, folder.ID, workspace.ResourceSearchEventCreated); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index ontology project folder: %s", err))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit ontology folder transaction: %s", err))
		return
	}
	writeJSON(w, http.StatusCreated, folder)
}

// UpdateProjectFolderPropagation updates the legacy folder-level
// "Propagate view requirements" compatibility setting. The setting is
// deprecation-bound: once disabled, it cannot be re-enabled.
func (h *ProjectsHandlers) UpdateProjectFolderPropagation(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	folderID, ok := parseUUIDParam(w, r, "folder_id", "folder_id")
	if !ok {
		return
	}
	var body models.UpdateProjectFolderPropagationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Enabled == nil {
		writeJSONErr(w, http.StatusBadRequest, "enabled is required")
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
	folder, err := loadProjectFolder(r.Context(), h.Pool, projectID, folderID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if folder == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project folder not found")
		return
	}
	enabled, disabledAt, status, message := resolveProjectPropagationPatch(
		folder.PropagateViewRequirementsEnabled,
		folder.PropagateViewRequirementsDisabledAt,
		*body.Enabled,
		true,
		time.Now(),
	)
	if status != 0 {
		writeJSONErr(w, status, message)
		return
	}
	markings := body.ViewRequirementMarkingRIDs
	if markings == nil {
		if enabled && len(folder.ViewRequirementMarkingRIDs) == 0 {
			inherited, err := inheritedViewRequirementMarkings(r.Context(), h.Pool, projectID, folder.ParentFolderID)
			if err != nil {
				writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to load inherited view requirements: %s", err))
				return
			}
			markings = inherited
		} else {
			markings = folder.ViewRequirementMarkingRIDs
		}
	}
	markingsJSON, err := jsonStringSlice(markings)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("encode view requirement markings: %s", err))
		return
	}
	previousPropagationMarkings := []string{}
	if folder.PropagateViewRequirementsEnabled {
		previousPropagationMarkings = folder.ViewRequirementMarkingRIDs
	}
	nextPropagationMarkings := []string{}
	if enabled {
		nextPropagationMarkings = markings
	}
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start folder propagation transaction: %s", err))
		return
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(r.Context(),
		`UPDATE ontology_project_folders
		    SET propagate_view_requirements_enabled = $3,
		        propagate_view_requirements_disabled_at = $4,
		        view_requirement_marking_rids = $5::jsonb,
		        updated_at = NOW()
		  WHERE project_id = $1 AND id = $2 AND is_deleted = FALSE`,
		projectID, folderID, enabled, disabledAt, markingsJSON,
	); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to update folder propagation setting: %s", err))
		return
	}
	if err := workspace.UpsertFolderSearchIndexTx(r.Context(), tx, folderID, workspace.ResourceSearchEventUpdated); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index folder propagation setting: %s", err))
		return
	}
	var propagationJob *models.ViewRequirementPropagationJob
	if !sameStringSlice(previousPropagationMarkings, nextPropagationMarkings) {
		propagationJob, err = insertViewRequirementPropagationJobTx(
			r.Context(),
			tx,
			projectID,
			viewReqParentFolder,
			folderID,
			folder.RID,
			claims.Sub,
			nextPropagationMarkings,
			previousPropagationMarkings,
		)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to enqueue view requirement propagation job: %s", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit folder propagation transaction: %s", err))
		return
	}
	h.launchViewRequirementPropagationJob(propagationJob)
	updated, err := loadProjectFolder(r.Context(), h.Pool, projectID, folderID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project folder not found")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ─── resource bindings ─────────────────────────────────────────────────

// ListProjectResources mirrors Rust `list_project_resources`.
func (h *ProjectsHandlers) ListProjectResources(w http.ResponseWriter, r *http.Request) {
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
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, id); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT project_id, resource_kind, resource_id, bound_by,
		        COALESCE(view_requirement_marking_rids, '[]'::jsonb),
		        created_at
		 FROM ontology_project_resources
		 WHERE project_id = $1 AND is_deleted = FALSE
		 ORDER BY created_at DESC`,
		id,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project resources: %s", err))
		return
	}
	defer rows.Close()

	out := make([]models.OntologyProjectResourceBinding, 0)
	for rows.Next() {
		var b models.OntologyProjectResourceBinding
		var markings []byte
		if err := rows.Scan(&b.ProjectID, &b.ResourceKind, &b.ResourceID, &b.BoundBy, &markings, &b.CreatedAt); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project resources: %s", err))
			return
		}
		b.ViewRequirementMarkingRIDs = decodeStringSliceJSON(markings)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list ontology project resources: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, models.ListOntologyProjectResourcesResponse{Data: out})
}

// BindProjectResource mirrors Rust `bind_project_resource`.
// The (resource_kind, resource_id) primary key makes the UPSERT
// idempotent: re-binding moves the resource between projects without
// orphan rows.
func (h *ProjectsHandlers) BindProjectResource(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	var body models.BindOntologyProjectResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	resourceKind, err := domain.ParseOntologyResourceKind(body.ResourceKind)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
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
	if _, err := domain.EnsureProjectEditAccess(r.Context(), h.Pool, claims, projectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	ownerID, err := domain.LoadResourceOwnerID(r.Context(), h.Pool, resourceKind, body.ResourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ownerID == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology resource not found")
		return
	}
	existingProjectID, err := domain.LoadResourceProjectID(r.Context(), h.Pool, resourceKind, body.ResourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to load ontology resource binding: %s", err))
		return
	}
	if err := domain.EnsureResourceManageAccess(r.Context(), h.Pool, claims, *ownerID, existingProjectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	_, viewRequirementMarkings, err := loadProjectPropagationSource(r.Context(), h.Pool, projectID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to load project propagation setting: %s", err))
		return
	}
	projectPropagates := project.PropagateViewRequirementsEnabled
	if !projectPropagates {
		viewRequirementMarkings = []string{}
	}
	viewRequirementMarkingsJSON, err := jsonStringSlice(viewRequirementMarkings)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("encode view requirement markings: %s", err))
		return
	}

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ontology_project_resources
		     (project_id, resource_kind, resource_id, bound_by, view_requirement_marking_rids)
		 VALUES ($1, $2, $3, $4, $5::jsonb)
		 ON CONFLICT (resource_kind, resource_id)
		 DO UPDATE SET project_id = EXCLUDED.project_id,
		               bound_by = EXCLUDED.bound_by,
		               view_requirement_marking_rids = EXCLUDED.view_requirement_marking_rids,
		               created_at = NOW()
		 RETURNING project_id, resource_kind, resource_id, bound_by,
		           COALESCE(view_requirement_marking_rids, '[]'::jsonb), created_at`,
		projectID, resourceKind.String(), body.ResourceID, claims.Sub, viewRequirementMarkingsJSON,
	)
	binding := &models.OntologyProjectResourceBinding{}
	var bindingMarkings []byte
	if err := row.Scan(&binding.ProjectID, &binding.ResourceKind, &binding.ResourceID,
		&binding.BoundBy, &bindingMarkings, &binding.CreatedAt); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to bind ontology resource to project: %s", err))
		return
	}
	binding.ViewRequirementMarkingRIDs = decodeStringSliceJSON(bindingMarkings)
	writeJSON(w, http.StatusOK, binding)
}

// UnbindProjectResource mirrors Rust `unbind_project_resource`. The
// route shape is `/projects/{id}/resources/{kind}/{resource_id}`.
func (h *ProjectsHandlers) UnbindProjectResource(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	resourceKind, err := domain.ParseOntologyResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, ok := parseUUIDParam(w, r, "resource_id", "resource_id")
	if !ok {
		return
	}
	if _, err := domain.EnsureProjectEditAccess(r.Context(), h.Pool, claims, projectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	row := h.Pool.QueryRow(r.Context(),
		`SELECT project_id, resource_kind, resource_id, bound_by,
		        COALESCE(view_requirement_marking_rids, '[]'::jsonb), created_at
		 FROM ontology_project_resources
		 WHERE project_id = $1 AND resource_kind = $2 AND resource_id = $3 AND is_deleted = FALSE`,
		projectID, resourceKind.String(), resourceID,
	)
	binding := &models.OntologyProjectResourceBinding{}
	var bindingMarkings []byte
	if err := row.Scan(&binding.ProjectID, &binding.ResourceKind, &binding.ResourceID,
		&binding.BoundBy, &bindingMarkings, &binding.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONErr(w, http.StatusNotFound, "ontology project resource binding not found")
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to load ontology project resource binding: %s", err))
		return
	}
	binding.ViewRequirementMarkingRIDs = decodeStringSliceJSON(bindingMarkings)

	ownerID, err := domain.LoadResourceOwnerID(r.Context(), h.Pool, resourceKind, resourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ownerID == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology resource not found")
		return
	}
	bound := binding.ProjectID
	if err := domain.EnsureResourceManageAccess(r.Context(), h.Pool, claims, *ownerID, &bound); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}

	cmd, err := h.Pool.Exec(r.Context(),
		`DELETE FROM ontology_project_resources
		 WHERE project_id = $1 AND resource_kind = $2 AND resource_id = $3`,
		projectID, resourceKind.String(), resourceID,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to unbind ontology resource from project: %s", err))
		return
	}
	if cmd.RowsAffected() == 0 {
		writeJSONErr(w, http.StatusNotFound, "ontology project resource binding not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── body helpers ──────────────────────────────────────────────────────

func readJSONBody(r *http.Request) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if r.Body == nil {
		return out, nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// optionalStringField extracts a JSON string at `key` if present and
// non-null. Returns nil/nil when absent or null. Returns an error when
// the value is a non-string JSON type.
func optionalStringField(raw map[string]json.RawMessage, key string) (*string, error) {
	value, present := raw[key]
	if !present {
		return nil, nil
	}
	if strings.TrimSpace(string(value)) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return nil, fmt.Errorf("%s must be a string", key)
	}
	return &s, nil
}
