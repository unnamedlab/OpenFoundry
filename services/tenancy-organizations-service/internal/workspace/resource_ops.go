package workspace

// resource_ops.go ports services/tenancy-organizations-service/src/handlers/resource_ops.rs.
//
// These endpoints are scoped to the *ontology* workspace surface for
// Phase 1 (projects, folders, resource bindings). Other resource kinds
// continue to expose their own move/rename APIs in their owning
// services; the workspace UI is expected to call those services
// directly when a non-ontology row is acted upon — the `/batch`
// endpoint will gain a router for that in a later phase.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/libs/core-models/rid"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

const DefaultTrashRetentionDays = 30

// MoveRequest is the body of POST /workspace/resources/{kind}/{id}/move.
type MoveRequest struct {
	// TargetFolderID is the destination folder. nil moves the resource
	// to the project root (or, for resource bindings, leaves the binding
	// without folder ownership — folder ownership for bindings is
	// reserved for a later phase).
	TargetFolderID *uuid.UUID `json:"target_folder_id,omitempty"`
	// TargetFolderRID is the canonical Compass parent folder RID. When it
	// equals the target project RID, the folder moves to the project root.
	TargetFolderRID *string `json:"target_folder_rid,omitempty"`
	// TargetProjectID is the destination project. Only meaningful for
	// resource bindings — folders cannot hop projects in Phase 1
	// because that requires a deep clone.
	TargetProjectID *uuid.UUID `json:"target_project_id,omitempty"`
	// TargetProjectRID is the canonical Compass target project RID.
	TargetProjectRID *string `json:"target_project_rid,omitempty"`
	// ConfirmAccessPolicyChange is required when a folder crosses a project
	// boundary because inherited project roles and folder grants can change.
	ConfirmAccessPolicyChange bool `json:"confirm_access_policy_change,omitempty"`
	// ConfirmMarkingChange is required when the target project has a different
	// compatible marking set.
	ConfirmMarkingChange bool `json:"confirm_marking_change,omitempty"`
}

// RenameRequest is the body of POST /workspace/resources/{kind}/{id}/rename.
type RenameRequest struct {
	Name string `json:"name"`
}

type folderMoveSnapshot struct {
	ID             uuid.UUID
	RID            string
	ProjectID      uuid.UUID
	ParentFolderID *uuid.UUID
}

type projectMoveSnapshot struct {
	ID                             uuid.UUID
	RID                            string
	MarkingRIDs                    []string
	ResourceLevelRoleGrantsAllowed bool
	DefaultRole                    string
}

// DuplicateRequest is the body of POST /workspace/resources/{kind}/{id}/duplicate.
type DuplicateRequest struct {
	NewName        *string    `json:"new_name,omitempty"`
	TargetFolderID *uuid.UUID `json:"target_folder_id,omitempty"`
}

// BatchAction is one entry in a /workspace/resources/batch payload.
type BatchAction struct {
	Op                        string     `json:"op"` // "move" | "delete" | "restore" | "purge"
	ResourceKind              string     `json:"resource_kind"`
	ResourceID                uuid.UUID  `json:"resource_id"`
	TargetFolderID            *uuid.UUID `json:"target_folder_id,omitempty"`
	TargetFolderRID           *string    `json:"target_folder_rid,omitempty"`
	TargetProjectID           *uuid.UUID `json:"target_project_id,omitempty"`
	TargetProjectRID          *string    `json:"target_project_rid,omitempty"`
	ConfirmAccessPolicyChange bool       `json:"confirm_access_policy_change,omitempty"`
	ConfirmMarkingChange      bool       `json:"confirm_marking_change,omitempty"`
	RetentionDays             *int       `json:"retention_days,omitempty"`
}

// BatchRequest is the body of POST /workspace/resources/batch.
type BatchRequest struct {
	Actions []BatchAction `json:"actions"`
}

// BatchResultEntry is the per-action outcome reported back to the UI.
type BatchResultEntry struct {
	Op           string    `json:"op"`
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
	OK           bool      `json:"ok"`
	Error        *string   `json:"error"`
}

// BatchResponse pins the {results: [...]} envelope.
type BatchResponse struct {
	Results []BatchResultEntry `json:"results"`
}

// ─── HTTP handlers ──────────────────────────────────────────────────

// MoveResource handles POST /api/v1/workspace/resources/{kind}/{id}/move.
//
// Folders can be re-parented within a project or moved to another project
// after explicit policy/marking confirmation. RIDs are never modified; path
// and breadcrumb views are derived from the updated project/parent chain.
func (h *Handlers) MoveResource(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	var body MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if kind == ResourceOntologyFolder && (body.TargetProjectID != nil || body.TargetProjectRID != nil) && !body.ConfirmAccessPolicyChange {
		writeJSONErr(w, http.StatusConflict, "moving a folder across projects changes inherited access policies; set confirm_access_policy_change=true")
		return
	}
	if status, msg := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}
	switch kind {
	case ResourceOntologyFolder:
		if err := h.Repo.moveFolder(r.Context(), claims, resourceID, body); err != nil {
			writeMoveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case ResourceOntologyResourceBinding:
		// Move a resource binding to a different project. We do not
		// model folder ownership for bindings yet, so target_folder_id
		// is currently ignored — kept in the API for forward-compat.
		if body.TargetProjectID == nil {
			writeJSONErr(w, http.StatusBadRequest,
				"'target_project_id' is required for resource bindings")
			return
		}
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_project_resources
			   SET project_id = $2
			   WHERE resource_id = $1 AND is_deleted = FALSE`,
			resourceID, *body.TargetProjectID)
		writeExecOutcome(w, ct, err, "failed to move resource binding")
	default:
		writeJSONErr(w, http.StatusBadRequest,
			fmt.Sprintf("move is not supported for resource_kind '%s'", kind))
	}
}

// RenameResource handles POST /api/v1/workspace/resources/{kind}/{id}/rename.
func (h *Handlers) RenameResource(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	var body RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	newName := strings.TrimSpace(body.Name)
	if newName == "" {
		writeJSONErr(w, http.StatusBadRequest, "'name' must not be empty")
		return
	}
	if status, msg := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}
	switch kind {
	case ResourceOntologyProject:
		tx, err := h.Repo.Pool.Begin(r.Context())
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start resource rename transaction: %s", err))
			return
		}
		defer tx.Rollback(context.Background())
		ct, err := tx.Exec(r.Context(),
			`UPDATE ontology_projects
			   SET display_name = $2, updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, newName)
		if err != nil {
			slog.Error("failed to rename project", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to rename project: %s", err))
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSONErr(w, http.StatusNotFound, "no row matched")
			return
		}
		if err := UpsertProjectSearchIndexTx(r.Context(), tx, resourceID, ResourceSearchEventUpdated); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index renamed project: %s", err))
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit resource rename transaction: %s", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case ResourceOntologyFolder:
		slug, err := folderSlugFromName(newName)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		tx, err := h.Repo.Pool.Begin(r.Context())
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start resource rename transaction: %s", err))
			return
		}
		defer tx.Rollback(context.Background())
		ct, err := tx.Exec(r.Context(),
			`UPDATE ontology_project_folders
			   SET name = $2, slug = $3, updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, newName, slug)
		if err != nil {
			slog.Error("failed to rename folder", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to rename folder: %s", err))
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSONErr(w, http.StatusNotFound, "no row matched")
			return
		}
		if err := UpsertFolderSearchIndexTx(r.Context(), tx, resourceID, ResourceSearchEventUpdated); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to index renamed folder: %s", err))
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit resource rename transaction: %s", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSONErr(w, http.StatusBadRequest,
			fmt.Sprintf("rename is not supported for resource_kind '%s'", kind))
	}
}

// DuplicateResource handles POST /api/v1/workspace/resources/{kind}/{id}/duplicate.
//
// Phase 1 only supports duplicating *folders* (shallow: the folder row
// is cloned with a new id; children are not copied). Duplicating
// projects or resource bindings requires a deeper clone routine that is
// out of scope here and deferred to Phase 2.
func (h *Handlers) DuplicateResource(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	var body DuplicateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if status, msg := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}

	switch kind {
	case ResourceOntologyFolder:
		newID := ids.New()
		newRID := models.FolderRIDFromID(newID)
		tx, err := h.Repo.Pool.Begin(r.Context())
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError,
				fmt.Sprintf("failed to start duplicate folder transaction: %s", err))
			return
		}
		defer tx.Rollback(context.Background())
		ct, err := tx.Exec(r.Context(),
			`INSERT INTO ontology_project_folders
			       (id, rid, project_id, parent_folder_id, name, slug, description, created_by)
			   SELECT $1,
			          $2,
			          project_id,
			          COALESCE($3, parent_folder_id),
			          COALESCE($4, name || ' (copy)'),
			          slug || '-' || substr($1::text, 1, 8),
			          description,
			          $5
			   FROM ontology_project_folders
			   WHERE id = $6 AND is_deleted = FALSE`,
			newID, newRID, body.TargetFolderID, body.NewName, claims.Sub, resourceID)
		if err != nil {
			slog.Error("duplicate folder", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError,
				fmt.Sprintf("failed to duplicate folder: %s", err))
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSONErr(w, http.StatusNotFound, "source folder not found")
			return
		}
		if err := UpsertFolderSearchIndexTx(r.Context(), tx, newID, ResourceSearchEventCreated); err != nil {
			writeJSONErr(w, http.StatusInternalServerError,
				fmt.Sprintf("failed to index duplicated folder: %s", err))
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSONErr(w, http.StatusInternalServerError,
				fmt.Sprintf("failed to commit duplicate folder transaction: %s", err))
			return
		}
		writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": newID})
	default:
		writeJSONErr(w, http.StatusBadRequest,
			fmt.Sprintf("duplicate is not supported for resource_kind '%s' in Phase 1", kind))
	}
}

// SoftDeleteResource handles DELETE /api/v1/workspace/resources/{kind}/{id}.
// Soft-delete sends the row to the trash. Hard delete is `…/purge` in the
// trash handler (TO-6).
func (h *Handlers) SoftDeleteResource(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	if status, msg := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}
	retentionDays, err := trashRetentionDaysFromRequest(r)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}

	switch kind {
	case ResourceOntologyProject, ResourceOntologyFolder, ResourceOntologyResourceBinding:
		tx, err := h.Repo.Pool.Begin(r.Context())
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to start resource delete transaction: %s", err))
			return
		}
		defer tx.Rollback(context.Background())
		rowsAffected, err := h.Repo.softDeleteOneTx(r.Context(), tx, claims.Sub, kind, resourceID, retentionDays)
		if err != nil {
			slog.Error("failed to delete resource", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete resource: %s", err))
			return
		}
		if rowsAffected == 0 {
			writeJSONErr(w, http.StatusNotFound, "no row matched")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit resource delete transaction: %s", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSONErr(w, http.StatusBadRequest,
			fmt.Sprintf("soft delete is not supported for resource_kind '%s'", kind))
	}
}

// BatchApply handles POST /api/v1/workspace/resources/batch.
//
// Each action is applied independently: there is no global transaction.
// One result entry is returned per input action so the UI can surface
// partial failures (matches the Rust impl byte-exact).
func (h *Handlers) BatchApply(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	results := make([]BatchResultEntry, 0, len(body.Actions))
	for _, action := range body.Actions {
		entry := BatchResultEntry{
			Op:           action.Op,
			ResourceKind: action.ResourceKind,
			ResourceID:   action.ResourceID,
		}
		kind, err := ParseResourceKind(action.ResourceKind)
		if err != nil {
			msg := err.Error()
			entry.Error = &msg
			results = append(results, entry)
			continue
		}

		var opErr error
		switch action.Op {
		case "delete":
			if status, _ := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, action.ResourceID); status != 0 {
				opErr = errors.New("forbidden")
			} else {
				retentionDays, err := normalizeTrashRetentionDays(action.RetentionDays)
				if err != nil {
					opErr = err
					break
				}
				opErr = h.Repo.softDeleteOne(r.Context(), claims.Sub, kind, action.ResourceID, retentionDays)
			}
		case "move":
			if status, _ := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, action.ResourceID); status != 0 {
				opErr = errors.New("forbidden")
				break
			}
			if kind != ResourceOntologyFolder {
				opErr = fmt.Errorf("batch move only supported for folders in Phase 1 (got '%s')", kind)
				break
			}
			opErr = h.Repo.moveFolder(r.Context(), claims, action.ResourceID, MoveRequest{
				TargetFolderID:            action.TargetFolderID,
				TargetFolderRID:           action.TargetFolderRID,
				TargetProjectID:           action.TargetProjectID,
				TargetProjectRID:          action.TargetProjectRID,
				ConfirmAccessPolicyChange: action.ConfirmAccessPolicyChange,
				ConfirmMarkingChange:      action.ConfirmMarkingChange,
			})
		default:
			opErr = fmt.Errorf("unsupported batch op '%s'", action.Op)
		}

		if opErr != nil {
			msg := opErr.Error()
			entry.Error = &msg
		} else {
			entry.OK = true
		}
		results = append(results, entry)
	}

	writeJSON(w, http.StatusOK, BatchResponse{Results: results})
}

// ─── Internal helpers ───────────────────────────────────────────────

type resourceOpError struct {
	status int
	msg    string
}

func (e *resourceOpError) Error() string { return e.msg }

func newResourceOpError(status int, msg string) error {
	return &resourceOpError{status: status, msg: msg}
}

func writeMoveError(w http.ResponseWriter, err error) {
	var opErr *resourceOpError
	if errors.As(err, &opErr) {
		writeJSONErr(w, opErr.status, opErr.msg)
		return
	}
	slog.Error("failed to move folder", slog.String("error", err.Error()))
	writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to move folder: %s", err))
}

func folderSlugFromName(value string) (string, error) {
	var slug strings.Builder
	lastWasDash := false
	for _, ch := range value {
		if isASCIIAlphaNum(ch) {
			if ch >= 'A' && ch <= 'Z' {
				ch += 'a' - 'A'
			}
			slug.WriteRune(ch)
			lastWasDash = false
			continue
		}
		if slug.Len() > 0 && !lastWasDash {
			slug.WriteByte('-')
			lastWasDash = true
		}
	}
	out := strings.Trim(slug.String(), "-")
	if out == "" {
		return "", errors.New("folder name must contain letters or numbers")
	}
	return out, nil
}

func isASCIIAlphaNum(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

func parseProjectRIDLocator(value, field string) (uuid.UUID, error) {
	parsed, err := rid.ParseUUID(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid project RID: %s", field, err)
	}
	if parsed.Service != "compass" || parsed.ResourceType != "project" {
		return uuid.Nil, fmt.Errorf("%s must be a compass project RID", field)
	}
	id, ok := parsed.UUID()
	if !ok {
		return uuid.Nil, fmt.Errorf("%s must carry a UUID locator", field)
	}
	return id, nil
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

func (r *Repo) moveFolder(ctx context.Context, claims *authmw.Claims, folderID uuid.UUID, body MoveRequest) error {
	sourceFolder, err := r.loadFolderMoveSnapshot(ctx, folderID)
	if err != nil {
		return err
	}
	if sourceFolder == nil {
		return newResourceOpError(http.StatusNotFound, "source folder not found")
	}
	sourceProject, err := r.loadProjectMoveSnapshot(ctx, sourceFolder.ProjectID)
	if err != nil {
		return err
	}
	if sourceProject == nil {
		return newResourceOpError(http.StatusNotFound, "source project not found")
	}

	targetProjectID := sourceFolder.ProjectID
	targetProjectExplicit := false
	if body.TargetProjectID != nil {
		targetProjectID = *body.TargetProjectID
		targetProjectExplicit = true
	}
	if body.TargetProjectRID != nil {
		id, err := parseProjectRIDLocator(*body.TargetProjectRID, "target_project_rid")
		if err != nil {
			return newResourceOpError(http.StatusBadRequest, err.Error())
		}
		if targetProjectExplicit && targetProjectID != id {
			return newResourceOpError(http.StatusBadRequest, "target_project_id and target_project_rid refer to different projects")
		}
		targetProjectID = id
		targetProjectExplicit = true
	}

	var targetFolderID *uuid.UUID
	if body.TargetFolderID != nil {
		target := *body.TargetFolderID
		targetFolderID = &target
	}
	if body.TargetFolderRID != nil {
		clean := strings.TrimSpace(*body.TargetFolderRID)
		if clean == "" {
			return newResourceOpError(http.StatusBadRequest, "target_folder_rid must be a non-empty RID")
		}
		targetProjectRID := models.ProjectRIDFromID(targetProjectID)
		if clean == targetProjectRID {
			targetFolderID = nil
		} else {
			id, err := parseFolderRIDLocator(clean, "target_folder_rid")
			if err != nil {
				return newResourceOpError(http.StatusBadRequest, err.Error())
			}
			if targetFolderID != nil && *targetFolderID != id {
				return newResourceOpError(http.StatusBadRequest, "target_folder_id and target_folder_rid refer to different folders")
			}
			targetFolderID = &id
		}
	}

	if targetFolderID != nil {
		if *targetFolderID == folderID {
			return newResourceOpError(http.StatusConflict, "cannot move a folder into itself")
		}
		parent, err := r.loadFolderMoveSnapshot(ctx, *targetFolderID)
		if err != nil {
			return err
		}
		if parent == nil {
			return newResourceOpError(http.StatusNotFound, "target folder not found")
		}
		if targetProjectExplicit && parent.ProjectID != targetProjectID {
			return newResourceOpError(http.StatusBadRequest, "target folder does not belong to target project")
		}
		targetProjectID = parent.ProjectID
		if parent.ProjectID == sourceFolder.ProjectID {
			descendant, err := r.isDescendantFolder(ctx, sourceFolder.ProjectID, folderID, *targetFolderID)
			if err != nil {
				return err
			}
			if descendant {
				return newResourceOpError(http.StatusConflict, "cannot move a folder into one of its descendants")
			}
		}
	}

	targetProject, err := r.loadProjectMoveSnapshot(ctx, targetProjectID)
	if err != nil {
		return err
	}
	if targetProject == nil {
		return newResourceOpError(http.StatusNotFound, "target project not found")
	}
	crossProject := targetProjectID != sourceFolder.ProjectID
	if crossProject {
		if status, msg := r.ensureOwnerOrAdmin(ctx, claims, ResourceOntologyProject, targetProjectID); status != 0 {
			return newResourceOpError(status, msg)
		}
		if !body.ConfirmAccessPolicyChange {
			return newResourceOpError(http.StatusConflict, "moving a folder across projects changes inherited access policies; set confirm_access_policy_change=true")
		}
		missing := missingStrings(sourceProject.MarkingRIDs, targetProject.MarkingRIDs)
		if len(missing) > 0 {
			return newResourceOpError(http.StatusConflict, fmt.Sprintf("target project markings are incompatible; missing: %s", strings.Join(missing, ", ")))
		}
		if !sameStringSet(sourceProject.MarkingRIDs, targetProject.MarkingRIDs) && !body.ConfirmMarkingChange {
			return newResourceOpError(http.StatusConflict, "moving a folder changes inherited markings; set confirm_marking_change=true")
		}
	}

	if crossProject {
		return r.moveFolderAcrossProjects(ctx, folderID, targetProjectID, targetFolderID)
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	ct, err := tx.Exec(ctx,
		`UPDATE ontology_project_folders
		   SET parent_folder_id = $2, updated_at = NOW()
		   WHERE id = $1 AND is_deleted = FALSE`,
		folderID, targetFolderID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return newResourceOpError(http.StatusNotFound, "source folder not found")
	}
	if err := UpsertFolderSearchIndexTx(ctx, tx, folderID, ResourceSearchEventMoved); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) loadFolderMoveSnapshot(ctx context.Context, folderID uuid.UUID) (*folderMoveSnapshot, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, rid, project_id, parent_folder_id
		   FROM ontology_project_folders
		  WHERE id = $1 AND is_deleted = FALSE`,
		folderID,
	)
	var f folderMoveSnapshot
	if err := row.Scan(&f.ID, &f.RID, &f.ProjectID, &f.ParentFolderID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(f.RID) == "" {
		f.RID = models.FolderRIDFromID(f.ID)
	}
	return &f, nil
}

func (r *Repo) loadProjectMoveSnapshot(ctx context.Context, projectID uuid.UUID) (*projectMoveSnapshot, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        COALESCE(marking_rids, '[]'::jsonb),
		        COALESCE(resource_level_role_grants_allowed, TRUE),
		        COALESCE(default_role, 'viewer')
		   FROM ontology_projects
		  WHERE id = $1 AND is_deleted = FALSE`,
		projectID,
	)
	var (
		p       projectMoveSnapshot
		rawJSON []byte
	)
	if err := row.Scan(&p.ID, &p.RID, &rawJSON, &p.ResourceLevelRoleGrantsAllowed, &p.DefaultRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(rawJSON, &p.MarkingRIDs); err != nil {
		return nil, fmt.Errorf("decode project marking_rids: %w", err)
	}
	if p.MarkingRIDs == nil {
		p.MarkingRIDs = []string{}
	}
	return &p, nil
}

func (r *Repo) isDescendantFolder(ctx context.Context, projectID, ancestorID, candidateID uuid.UUID) (bool, error) {
	row := r.Pool.QueryRow(ctx,
		`WITH RECURSIVE descendants AS (
		     SELECT id
		       FROM ontology_project_folders
		      WHERE project_id = $1 AND parent_folder_id = $2 AND is_deleted = FALSE
		     UNION ALL
		     SELECT f.id
		       FROM ontology_project_folders f
		       JOIN descendants d ON f.parent_folder_id = d.id
		      WHERE f.project_id = $1 AND f.is_deleted = FALSE
		 )
		 SELECT EXISTS(SELECT 1 FROM descendants WHERE id = $3)`,
		projectID, ancestorID, candidateID,
	)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repo) moveFolderAcrossProjects(ctx context.Context, folderID, targetProjectID uuid.UUID, targetFolderID *uuid.UUID) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	rows, err := tx.Query(ctx,
		`WITH RECURSIVE moved AS (
		     SELECT id
		       FROM ontology_project_folders
		      WHERE id = $1 AND is_deleted = FALSE
		     UNION ALL
		     SELECT f.id
		       FROM ontology_project_folders f
		       JOIN moved m ON f.parent_folder_id = m.id
		      WHERE f.is_deleted = FALSE
		 )
		 SELECT id FROM moved`,
		folderID,
	)
	if err != nil {
		return err
	}
	movedIDs := make([]uuid.UUID, 0, 8)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		movedIDs = append(movedIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(movedIDs) == 0 {
		return newResourceOpError(http.StatusNotFound, "source folder not found")
	}

	ct, err := tx.Exec(ctx,
		`UPDATE ontology_project_folders
		    SET project_id = $2,
		        parent_folder_id = CASE WHEN id = $1 THEN $3 ELSE parent_folder_id END,
		        updated_at = NOW()
		  WHERE id = ANY($4) AND is_deleted = FALSE`,
		folderID, targetProjectID, targetFolderID, movedIDs,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return newResourceOpError(http.StatusNotFound, "source folder not found")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ontology_project_resource_grants
		    SET project_id = $2, updated_at = NOW()
		  WHERE scope_kind = 'folder' AND scope_id = ANY($1)`,
		movedIDs, targetProjectID,
	); err != nil {
		return err
	}
	for _, movedID := range movedIDs {
		if err := UpsertFolderSearchIndexTx(ctx, tx, movedID, ResourceSearchEventMoved); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func sameStringSet(a, b []string) bool {
	return len(missingStrings(a, b)) == 0 && len(missingStrings(b, a)) == 0
}

func missingStrings(required, available []string) []string {
	seen := make(map[string]struct{}, len(available))
	for _, value := range available {
		seen[value] = struct{}{}
	}
	missing := make([]string, 0)
	for _, value := range required {
		if _, ok := seen[value]; !ok {
			missing = append(missing, value)
		}
	}
	return missing
}

// writeExecOutcome maps a pgx.CommandTag/error pair into the standard
// 204/404/500 envelopes used by the Rust resource_ops handler.
type rowsAffectedTag interface{ RowsAffected() int64 }

func writeExecOutcome(w http.ResponseWriter, ct rowsAffectedTag, err error, failureMsg string) {
	if err != nil {
		slog.Error(failureMsg, slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError,
			fmt.Sprintf("%s: %s", failureMsg, err))
		return
	}
	if ct == nil || ct.RowsAffected() == 0 {
		writeJSONErr(w, http.StatusNotFound, "no row matched")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// softDeleteOne runs the same UPDATE as SoftDeleteResource but in the
// batch path, where no per-row response envelope is emitted.
func (r *Repo) softDeleteOne(ctx context.Context, actor uuid.UUID, kind ResourceKind, resourceID uuid.UUID, retentionDays int) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err := r.softDeleteOneTx(ctx, tx, actor, kind, resourceID, retentionDays); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) softDeleteOneTx(ctx context.Context, tx pgx.Tx, actor uuid.UUID, kind ResourceKind, resourceID uuid.UUID, retentionDays int) (int64, error) {
	switch kind {
	case ResourceOntologyProject:
		ct, err := tx.Exec(ctx,
			`UPDATE ontology_projects
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       trash_retention_days = $3,
			       purge_after = NOW() + ($3::int * INTERVAL '1 day'),
			       original_project_id = NULL,
			       original_parent_folder_id = NULL,
			       updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, actor, retentionDays)
		if err != nil || ct.RowsAffected() == 0 {
			return ct.RowsAffected(), err
		}
		if err := UpsertProjectSearchIndexTx(ctx, tx, resourceID, ResourceSearchEventTrashed); err != nil {
			return 0, err
		}
		return ct.RowsAffected(), nil
	case ResourceOntologyFolder:
		ct, err := tx.Exec(ctx,
			`UPDATE ontology_project_folders
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       trash_retention_days = $3,
			       purge_after = NOW() + ($3::int * INTERVAL '1 day'),
			       original_project_id = project_id,
			       original_parent_folder_id = parent_folder_id,
			       updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, actor, retentionDays)
		if err != nil || ct.RowsAffected() == 0 {
			return ct.RowsAffected(), err
		}
		if err := UpsertFolderSearchIndexTx(ctx, tx, resourceID, ResourceSearchEventTrashed); err != nil {
			return 0, err
		}
		return ct.RowsAffected(), nil
	case ResourceOntologyResourceBinding:
		ct, err := tx.Exec(ctx,
			`UPDATE ontology_project_resources
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       trash_retention_days = $3,
			       purge_after = NOW() + ($3::int * INTERVAL '1 day'),
			       original_project_id = project_id,
			       original_parent_folder_id = NULL
			   WHERE resource_id = $1 AND is_deleted = FALSE`,
			resourceID, actor, retentionDays)
		return ct.RowsAffected(), err
	}
	// Unsupported kinds are filtered before reaching here in BatchApply.
	return 0, nil
}

func trashRetentionDaysFromRequest(r *http.Request) (int, error) {
	days, err := normalizeTrashRetentionDays(nil)
	if err != nil {
		return 0, err
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("retention_days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("retention_days must be an integer")
		}
		days, err = normalizeTrashRetentionDays(&n)
		if err != nil {
			return 0, err
		}
	}
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return days, nil
	}
	var body struct {
		RetentionDays *int `json:"retention_days,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return days, nil
		}
		return 0, fmt.Errorf("invalid trash body")
	}
	if body.RetentionDays == nil {
		return days, nil
	}
	return normalizeTrashRetentionDays(body.RetentionDays)
}

func normalizeTrashRetentionDays(value *int) (int, error) {
	days := DefaultTrashRetentionDays
	if value != nil {
		days = *value
	}
	if days < 1 || days > 3650 {
		return 0, fmt.Errorf("retention_days must be between 1 and 3650")
	}
	return days, nil
}

// ensureOwnerOrAdmin authorises a single resource_ops action.
//
// Returns (0, "") when the caller may proceed. Otherwise returns the
// HTTP status + JSON error message the handler should write back. The
// status taxonomy mirrors the Rust impl exactly:
//
//   - 400 for unsupported kinds (forwarded by the caller as `bad`).
//   - 403 when the caller is neither admin nor the project owner.
//   - 404 when the resource does not exist.
//   - 500 on database errors.
func (r *Repo) ensureOwnerOrAdmin(ctx context.Context, claims *authmw.Claims, kind ResourceKind, resourceID uuid.UUID) (int, string) {
	if claims.HasRole("admin") {
		return 0, ""
	}
	var (
		owner uuid.UUID
		err   error
	)
	switch kind {
	case ResourceOntologyProject:
		err = r.Pool.QueryRow(ctx,
			`SELECT owner_id FROM ontology_projects WHERE id = $1`,
			resourceID).Scan(&owner)
	case ResourceOntologyFolder:
		err = r.Pool.QueryRow(ctx,
			`SELECT p.owner_id
			   FROM ontology_project_folders f
			   JOIN ontology_projects p ON p.id = f.project_id
			   WHERE f.id = $1`,
			resourceID).Scan(&owner)
	case ResourceOntologyResourceBinding:
		err = r.Pool.QueryRow(ctx,
			`SELECT p.owner_id
			   FROM ontology_project_resources r
			   JOIN ontology_projects p ON p.id = r.project_id
			   WHERE r.resource_id = $1`,
			resourceID).Scan(&owner)
	default:
		return http.StatusBadRequest,
			fmt.Sprintf("operation not supported for resource_kind '%s'", kind)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound, "resource not found"
	}
	if err != nil {
		slog.Error("load resource owner", slog.String("error", err.Error()))
		return http.StatusInternalServerError,
			fmt.Sprintf("failed to load resource owner: %s", err)
	}
	if owner == claims.Sub {
		return 0, ""
	}
	return http.StatusForbidden,
		"only the project owner or an admin may perform this action"
}
