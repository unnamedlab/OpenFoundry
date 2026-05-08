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
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
)

// MoveRequest is the body of POST /workspace/resources/{kind}/{id}/move.
type MoveRequest struct {
	// TargetFolderID is the destination folder. nil moves the resource
	// to the project root (or, for resource bindings, leaves the binding
	// without folder ownership — folder ownership for bindings is
	// reserved for a later phase).
	TargetFolderID *uuid.UUID `json:"target_folder_id,omitempty"`
	// TargetProjectID is the destination project. Only meaningful for
	// resource bindings — folders cannot hop projects in Phase 1
	// because that requires a deep clone.
	TargetProjectID *uuid.UUID `json:"target_project_id,omitempty"`
}

// RenameRequest is the body of POST /workspace/resources/{kind}/{id}/rename.
type RenameRequest struct {
	Name string `json:"name"`
}

// DuplicateRequest is the body of POST /workspace/resources/{kind}/{id}/duplicate.
type DuplicateRequest struct {
	NewName        *string    `json:"new_name,omitempty"`
	TargetFolderID *uuid.UUID `json:"target_folder_id,omitempty"`
}

// BatchAction is one entry in a /workspace/resources/batch payload.
type BatchAction struct {
	Op             string     `json:"op"` // "move" | "delete" | "restore" | "purge"
	ResourceKind   string     `json:"resource_kind"`
	ResourceID     uuid.UUID  `json:"resource_id"`
	TargetFolderID *uuid.UUID `json:"target_folder_id,omitempty"`
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
// Phase 1: only folders (re-parent within a project) and resource
// bindings (project hop) are supported. Project moves require deep
// clone semantics that are deferred.
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
	if status, msg := h.Repo.ensureOwnerOrAdmin(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}

	switch kind {
	case ResourceOntologyFolder:
		// Reparent within the same project. parent_folder_id may be NULL.
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_project_folders
			   SET parent_folder_id = $2, updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, body.TargetFolderID)
		writeExecOutcome(w, ct, err, "failed to move folder")
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
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_projects
			   SET display_name = $2, updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, newName)
		writeExecOutcome(w, ct, err, "failed to rename project")
	case ResourceOntologyFolder:
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_project_folders
			   SET name = $2, updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, newName)
		writeExecOutcome(w, ct, err, "failed to rename folder")
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
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`INSERT INTO ontology_project_folders
			       (id, project_id, parent_folder_id, name, slug, description, created_by)
			   SELECT $1,
			          project_id,
			          COALESCE($2, parent_folder_id),
			          COALESCE($3, name || ' (copy)'),
			          slug || '-' || substr($1::text, 1, 8),
			          description,
			          $4
			   FROM ontology_project_folders
			   WHERE id = $5 AND is_deleted = FALSE`,
			newID, body.TargetFolderID, body.NewName, claims.Sub, resourceID)
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

	switch kind {
	case ResourceOntologyProject:
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_projects
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, claims.Sub)
		writeExecOutcome(w, ct, err, "failed to delete resource")
	case ResourceOntologyFolder:
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_project_folders
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       updated_at = NOW()
			   WHERE id = $1 AND is_deleted = FALSE`,
			resourceID, claims.Sub)
		writeExecOutcome(w, ct, err, "failed to delete resource")
	case ResourceOntologyResourceBinding:
		ct, err := h.Repo.Pool.Exec(r.Context(),
			`UPDATE ontology_project_resources
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2
			   WHERE resource_id = $1 AND is_deleted = FALSE`,
			resourceID, claims.Sub)
		writeExecOutcome(w, ct, err, "failed to delete resource")
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
				opErr = h.Repo.softDeleteOne(r.Context(), claims.Sub, kind, action.ResourceID)
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
			_, opErr = h.Repo.Pool.Exec(r.Context(),
				`UPDATE ontology_project_folders
				   SET parent_folder_id = $2, updated_at = NOW()
				   WHERE id = $1 AND is_deleted = FALSE`,
				action.ResourceID, action.TargetFolderID)
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
func (r *Repo) softDeleteOne(ctx context.Context, actor uuid.UUID, kind ResourceKind, resourceID uuid.UUID) error {
	switch kind {
	case ResourceOntologyProject:
		_, err := r.Pool.Exec(ctx,
			`UPDATE ontology_projects
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       updated_at = NOW()
			   WHERE id = $1`,
			resourceID, actor)
		return err
	case ResourceOntologyFolder:
		_, err := r.Pool.Exec(ctx,
			`UPDATE ontology_project_folders
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
			       updated_at = NOW()
			   WHERE id = $1`,
			resourceID, actor)
		return err
	case ResourceOntologyResourceBinding:
		_, err := r.Pool.Exec(ctx,
			`UPDATE ontology_project_resources
			   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2
			   WHERE resource_id = $1`,
			resourceID, actor)
		return err
	}
	// Unsupported kinds are filtered before reaching here in BatchApply.
	return nil
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
