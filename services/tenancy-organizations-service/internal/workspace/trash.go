package workspace

// trash.go ports services/tenancy-organizations-service/src/handlers/trash.rs.
//
// Trash UX (soft-delete + restore + purge) for the ontology workspace
// resources (projects, folders, resource bindings). Other resource
// kinds keep their own soft-delete mechanics in their own services;
// this handler only owns the ontology surface used by the workspace UI.
//
// Soft-delete itself is handled by SoftDeleteResource in resource_ops.go;
// here we expose:
//
//   - GET    /api/v1/workspace/trash
//   - POST   /api/v1/workspace/resources/{kind}/{id}/restore
//   - DELETE /api/v1/workspace/resources/{kind}/{id}/purge
//
// Soft-delete records the retention window and purge_after timestamp.
// A later reaper can hard-delete expired rows; the interactive purge
// endpoint remains explicit and guarded by `is_deleted = TRUE`.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// TrashEntry mirrors the Rust struct returned by `GET /workspace/trash`.
//
// `ProjectID` is nullable because trashed projects do not nest under
// another project, only folders/bindings do.
// `DeletedBy` is nullable to mirror legacy rows whose deleter id was
// never recorded (Rust column is `deleted_by UUID NULL`).
type TrashEntry struct {
	ResourceKind           string     `json:"resource_kind"`
	ResourceID             uuid.UUID  `json:"resource_id"`
	ProjectID              *uuid.UUID `json:"project_id"`
	DisplayName            string     `json:"display_name"`
	DeletedAt              time.Time  `json:"deleted_at"`
	DeletedBy              *uuid.UUID `json:"deleted_by"`
	RetentionDays          int        `json:"retention_days"`
	PurgeAfter             *time.Time `json:"purge_after"`
	OriginalProjectID      *uuid.UUID `json:"original_project_id"`
	OriginalParentFolderID *uuid.UUID `json:"original_parent_folder_id"`
	RestoreTargetStatus    string     `json:"restore_target_status"`
}

// ListTrashResponse pins the {data:[...]} envelope used across the
// workspace surface (matches Rust impl).
type ListTrashResponse struct {
	Data []TrashEntry `json:"data"`
}

type RestoreResourceResponse struct {
	Restored               bool       `json:"restored"`
	RestoredToOriginalPath bool       `json:"restored_to_original_path"`
	RestoredToProjectID    *uuid.UUID `json:"restored_to_project_id,omitempty"`
	RestoredToFolderID     *uuid.UUID `json:"restored_to_folder_id,omitempty"`
	Banner                 *string    `json:"banner,omitempty"`
}

type RestoreResult struct {
	RowsAffected           int64
	RestoredToOriginalPath bool
	RestoredToProjectID    *uuid.UUID
	RestoredToFolderID     *uuid.UUID
	Banner                 *string
}

// ─── HTTP handlers ──────────────────────────────────────────────────

// ListTrash handles GET /api/v1/workspace/trash?kind=…&limit=N.
//
// Admins see every soft-deleted row across the three ontology tables.
// Non-admins only see rows they themselves trashed (deleted_by = caller).
// The merge across kinds happens application-side: one query per
// soft-deletable table, then sort by deleted_at DESC and truncate to
// the requested limit (matches the Rust behaviour byte-exact).
func (h *Handlers) ListTrash(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit := parseLimit(r, 200, 1, 1000)

	var kindFilter ResourceKind
	if raw := r.URL.Query().Get("kind"); raw != "" {
		k, err := ParseResourceKind(raw)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		kindFilter = k
	}

	isAdmin := claims.HasRole("admin")
	entries, err := h.Repo.ListTrash(r.Context(), claims.Sub, isAdmin, kindFilter, limit)
	if err != nil {
		slog.Error("list trash", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list trash")
		return
	}
	writeJSON(w, http.StatusOK, ListTrashResponse{Data: entries})
}

// RestoreResource handles POST /api/v1/workspace/resources/{kind}/{id}/restore.
//
// Clears the soft-delete flags on a trashed row. Returns
// `{"restored": true}` on success. 404 when no trashed row matched
// (e.g. the row was already restored, never trashed, or never existed).
func (h *Handlers) RestoreResource(w http.ResponseWriter, r *http.Request) {
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
	if !IsTrashKindSupported(kind) {
		writeJSONErr(w, http.StatusUnprocessableEntity, ErrResourceKindUnsupported.Error())
		return
	}
	if status, msg := h.Repo.ensureCanModifyTrashed(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}

	result, err := h.Repo.RestoreTrashed(r.Context(), kind, resourceID)
	if err != nil {
		slog.Error("restore resource", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to restore resource")
		return
	}
	if result.RowsAffected == 0 {
		writeJSONErr(w, http.StatusNotFound, "no trashed row matched")
		return
	}
	writeJSON(w, http.StatusOK, RestoreResourceResponse{
		Restored:               true,
		RestoredToOriginalPath: result.RestoredToOriginalPath,
		RestoredToProjectID:    result.RestoredToProjectID,
		RestoredToFolderID:     result.RestoredToFolderID,
		Banner:                 result.Banner,
	})
}

// PurgeResource handles DELETE /api/v1/workspace/resources/{kind}/{id}/purge.
//
// Hard-delete a previously soft-deleted row. The DELETE includes
// `is_deleted = TRUE` so calling purge on a live row is a no-op (404).
func (h *Handlers) PurgeResource(w http.ResponseWriter, r *http.Request) {
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
	if !IsTrashKindSupported(kind) {
		writeJSONErr(w, http.StatusUnprocessableEntity, ErrResourceKindUnsupported.Error())
		return
	}
	if status, msg := h.Repo.ensureCanModifyTrashed(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}

	rowsAffected, err := h.Repo.PurgeTrashed(r.Context(), kind, resourceID)
	if err != nil {
		slog.Error("purge resource", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to purge resource")
		return
	}
	if rowsAffected == 0 {
		writeJSONErr(w, http.StatusNotFound, "no trashed row matched")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Repo surface ───────────────────────────────────────────────────

// ListTrash UNION-style merges trashed rows across the three ontology
// tables. Each per-kind query is bounded by `limit`; the application
// then sorts by deleted_at DESC and truncates to `limit` so the global
// budget is respected.
func (r *Repo) ListTrash(ctx context.Context, userID uuid.UUID, isAdmin bool, kind ResourceKind, limit int) ([]TrashEntry, error) {
	limit = clamp(limit, 1, 1000)
	entries := make([]TrashEntry, 0)

	if kind == "" || kind == ResourceOntologyProject {
		rows, err := r.Pool.Query(ctx,
			`SELECT id, NULL::uuid AS project_id, display_name, deleted_at, deleted_by,
			        COALESCE(trash_retention_days, $4) AS retention_days,
			        COALESCE(purge_after, deleted_at + (COALESCE(trash_retention_days, $4) * INTERVAL '1 day')) AS purge_after,
			        original_project_id,
			        original_parent_folder_id,
			        'original_path' AS restore_target_status
			   FROM ontology_projects
			  WHERE is_deleted = TRUE AND ($1 OR deleted_by = $2)
			  ORDER BY deleted_at DESC LIMIT $3`,
			isAdmin, userID, limit, DefaultTrashRetentionDays)
		if err != nil {
			return nil, fmt.Errorf("list trashed projects: %w", err)
		}
		out, err := scanTrashEntries(rows, string(ResourceOntologyProject))
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("list trashed projects: %w", err)
		}
		entries = append(entries, out...)
	}

	if kind == "" || kind == ResourceOntologyFolder {
		rows, err := r.Pool.Query(ctx,
			`SELECT f.id, f.project_id, f.name AS display_name, f.deleted_at, f.deleted_by,
			        COALESCE(f.trash_retention_days, $4) AS retention_days,
			        COALESCE(f.purge_after, f.deleted_at + (COALESCE(f.trash_retention_days, $4) * INTERVAL '1 day')) AS purge_after,
			        f.original_project_id,
			        f.original_parent_folder_id,
			        CASE
			            WHEN f.parent_folder_id IS NULL AND f.original_parent_folder_id IS NOT NULL THEN 'project_root'
			            WHEN f.parent_folder_id IS NOT NULL AND (parent.id IS NULL OR parent.is_deleted = TRUE) THEN 'project_root'
			            ELSE 'original_path'
			        END AS restore_target_status
			   FROM ontology_project_folders f
			   LEFT JOIN ontology_project_folders parent ON parent.id = f.parent_folder_id
			  WHERE f.is_deleted = TRUE AND ($1 OR f.deleted_by = $2)
			  ORDER BY f.deleted_at DESC LIMIT $3`,
			isAdmin, userID, limit, DefaultTrashRetentionDays)
		if err != nil {
			return nil, fmt.Errorf("list trashed folders: %w", err)
		}
		out, err := scanTrashEntries(rows, string(ResourceOntologyFolder))
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("list trashed folders: %w", err)
		}
		entries = append(entries, out...)
	}

	if kind == "" || kind == ResourceOntologyResourceBinding {
		// For bindings the SQL display_name slot is filled with
		// resource_kind so the UI can render "dataset · …" without an
		// extra round-trip to resource-resolve.
		rows, err := r.Pool.Query(ctx,
			`SELECT resource_id, project_id, resource_kind AS display_name, deleted_at, deleted_by,
			        COALESCE(trash_retention_days, $4) AS retention_days,
			        COALESCE(purge_after, deleted_at + (COALESCE(trash_retention_days, $4) * INTERVAL '1 day')) AS purge_after,
			        original_project_id,
			        original_parent_folder_id,
			        'original_path' AS restore_target_status
			   FROM ontology_project_resources
			  WHERE is_deleted = TRUE AND ($1 OR deleted_by = $2)
			  ORDER BY deleted_at DESC LIMIT $3`,
			isAdmin, userID, limit, DefaultTrashRetentionDays)
		if err != nil {
			return nil, fmt.Errorf("list trashed resource bindings: %w", err)
		}
		out, err := scanTrashEntries(rows, string(ResourceOntologyResourceBinding))
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("list trashed resource bindings: %w", err)
		}
		entries = append(entries, out...)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].DeletedAt.After(entries[j].DeletedAt)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// RestoreTrashed clears the soft-delete columns and reports whether the
// resource could be restored to its original path. Folders whose original
// parent was purged or is still in Trash are restored to the project root.
func (r *Repo) RestoreTrashed(ctx context.Context, kind ResourceKind, resourceID uuid.UUID) (RestoreResult, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return RestoreResult{}, err
	}
	defer tx.Rollback(context.Background())

	result := RestoreResult{RestoredToOriginalPath: true}
	switch kind {
	case ResourceOntologyProject:
		ct, err := tx.Exec(ctx,
			`UPDATE ontology_projects
			    SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
			        purge_after = NULL,
			        original_project_id = NULL,
			        original_parent_folder_id = NULL,
			        updated_at = NOW()
			  WHERE id = $1 AND is_deleted = TRUE`,
			resourceID)
		if err != nil {
			return RestoreResult{}, err
		}
		result.RowsAffected = ct.RowsAffected()
		if result.RowsAffected > 0 {
			result.RestoredToProjectID = &resourceID
			if err := UpsertProjectSearchIndexTx(ctx, tx, resourceID, ResourceSearchEventRestored); err != nil {
				return RestoreResult{}, err
			}
		}
	case ResourceOntologyFolder:
		placement, err := r.folderRestorePlacement(ctx, tx, resourceID)
		if err != nil {
			return RestoreResult{}, err
		}
		result.RestoredToProjectID = &placement.ProjectID
		result.RestoredToFolderID = placement.TargetParentFolderID
		result.RestoredToOriginalPath = placement.OriginalPath
		result.Banner = placement.Banner
		ct, err := tx.Exec(ctx,
			`UPDATE ontology_project_folders
			    SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
			        purge_after = NULL,
			        original_project_id = NULL,
			        original_parent_folder_id = NULL,
			        parent_folder_id = $2,
			        updated_at = NOW()
			  WHERE id = $1 AND is_deleted = TRUE`,
			resourceID, placement.TargetParentFolderID)
		if err != nil {
			return RestoreResult{}, err
		}
		result.RowsAffected = ct.RowsAffected()
		if result.RowsAffected > 0 {
			if err := UpsertFolderSearchIndexTx(ctx, tx, resourceID, ResourceSearchEventRestored); err != nil {
				return RestoreResult{}, err
			}
		}
	case ResourceOntologyResourceBinding:
		projectID, err := r.bindingRestoreProject(ctx, tx, resourceID)
		if err != nil {
			return RestoreResult{}, err
		}
		result.RestoredToProjectID = projectID
		ct, err := tx.Exec(ctx,
			`UPDATE ontology_project_resources
			    SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
			        purge_after = NULL,
			        original_project_id = NULL,
			        original_parent_folder_id = NULL
			  WHERE resource_id = $1 AND is_deleted = TRUE`,
			resourceID)
		if err != nil {
			return RestoreResult{}, err
		}
		result.RowsAffected = ct.RowsAffected()
	default:
		return RestoreResult{}, fmt.Errorf("restore is not implemented for resource_kind '%s'", kind)
	}
	if err := tx.Commit(ctx); err != nil {
		return RestoreResult{}, err
	}
	return result, nil
}

// PurgeTrashed hard-deletes a previously soft-deleted row. The
// `is_deleted = TRUE` guard ensures live rows can never be purged
// through this endpoint — destructive deletes go through
// SoftDeleteResource → PurgeResource, never directly.
func (r *Repo) PurgeTrashed(ctx context.Context, kind ResourceKind, resourceID uuid.UUID) (int64, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())

	var rowsAffected int64
	switch kind {
	case ResourceOntologyProject:
		if err := DeleteProjectSearchIndexTx(ctx, tx, resourceID, ResourceSearchEventPurged); err != nil {
			return 0, err
		}
		ct, err := tx.Exec(ctx,
			`DELETE FROM ontology_projects WHERE id = $1 AND is_deleted = TRUE`,
			resourceID)
		if err != nil {
			return 0, err
		}
		rowsAffected = ct.RowsAffected()
	case ResourceOntologyFolder:
		if err := DeleteFolderSearchIndexTx(ctx, tx, resourceID, ResourceSearchEventPurged); err != nil {
			return 0, err
		}
		ct, err := tx.Exec(ctx,
			`DELETE FROM ontology_project_folders WHERE id = $1 AND is_deleted = TRUE`,
			resourceID)
		if err != nil {
			return 0, err
		}
		rowsAffected = ct.RowsAffected()
	case ResourceOntologyResourceBinding:
		ct, err := tx.Exec(ctx,
			`DELETE FROM ontology_project_resources WHERE resource_id = $1 AND is_deleted = TRUE`,
			resourceID)
		if err != nil {
			return 0, err
		}
		rowsAffected = ct.RowsAffected()
	default:
		return 0, fmt.Errorf("purge is not implemented for resource_kind '%s'", kind)
	}
	if rowsAffected == 0 {
		return 0, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

// ensureCanModifyTrashed authorises restore/purge.
//
// Returns (0, "") when the caller may proceed. Otherwise returns the
// HTTP status + JSON error message the handler should write back.
//
// Access rule (mirrors Rust ensure_can_modify_trashed exactly):
//   - admin role            → allowed.
//   - project owner         → allowed.
//   - the user who deleted  → allowed.
//   - everyone else         → 403.
//
// This is intentionally broader than `ensureOwnerOrAdmin` from
// resource_ops.go: a non-owner who soft-deleted their own contribution
// still needs a path to restore or purge it.
func (r *Repo) ensureCanModifyTrashed(ctx context.Context, claims *authmw.Claims, kind ResourceKind, resourceID uuid.UUID) (int, string) {
	if claims.HasRole("admin") {
		return 0, ""
	}
	var (
		ownerID   uuid.UUID
		deletedBy *uuid.UUID
		err       error
	)
	switch kind {
	case ResourceOntologyProject:
		err = r.Pool.QueryRow(ctx,
			`SELECT owner_id, deleted_by FROM ontology_projects WHERE id = $1`,
			resourceID).Scan(&ownerID, &deletedBy)
	case ResourceOntologyFolder:
		err = r.Pool.QueryRow(ctx,
			`SELECT p.owner_id, f.deleted_by
			   FROM ontology_project_folders f
			   JOIN ontology_projects p ON p.id = f.project_id
			  WHERE f.id = $1`,
			resourceID).Scan(&ownerID, &deletedBy)
	case ResourceOntologyResourceBinding:
		err = r.Pool.QueryRow(ctx,
			`SELECT p.owner_id, r.deleted_by
			   FROM ontology_project_resources r
			   JOIN ontology_projects p ON p.id = r.project_id
			  WHERE r.resource_id = $1`,
			resourceID).Scan(&ownerID, &deletedBy)
	default:
		return http.StatusBadRequest,
			fmt.Sprintf("trash actions are not supported for '%s'", kind)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound, "resource not found"
	}
	if err != nil {
		slog.Error("load resource for trash action", slog.String("error", err.Error()))
		return http.StatusInternalServerError,
			fmt.Sprintf("failed to load resource for trash action: %s", err)
	}
	if ownerID == claims.Sub {
		return 0, ""
	}
	if deletedBy != nil && *deletedBy == claims.Sub {
		return 0, ""
	}
	return http.StatusForbidden,
		"only the owner or the user who deleted the resource may restore or purge it"
}

type folderRestorePlacement struct {
	ProjectID            uuid.UUID
	TargetParentFolderID *uuid.UUID
	OriginalPath         bool
	Banner               *string
}

func (r *Repo) folderRestorePlacement(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) (folderRestorePlacement, error) {
	var (
		placement       folderRestorePlacement
		parentFolderID  *uuid.UUID
		originalParent  *uuid.UUID
		parentAvailable bool
	)
	err := tx.QueryRow(ctx,
		`SELECT f.project_id,
		        f.parent_folder_id,
		        f.original_parent_folder_id,
		        CASE
		            WHEN f.parent_folder_id IS NULL THEN TRUE
		            ELSE EXISTS (
		                SELECT 1
		                  FROM ontology_project_folders parent
		                 WHERE parent.id = f.parent_folder_id
		                   AND parent.project_id = f.project_id
		                   AND parent.is_deleted = FALSE
		            )
		        END AS parent_available
		   FROM ontology_project_folders f
		  WHERE f.id = $1 AND f.is_deleted = TRUE`,
		folderID,
	).Scan(&placement.ProjectID, &parentFolderID, &originalParent, &parentAvailable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return placement, nil
		}
		return placement, err
	}
	placement.TargetParentFolderID = parentFolderID
	placement.OriginalPath = true
	if parentFolderID != nil && !parentAvailable {
		placement.TargetParentFolderID = nil
		placement.OriginalPath = false
		msg := "Original folder path is no longer available; restored to the project root."
		placement.Banner = &msg
	}
	if parentFolderID == nil && originalParent != nil {
		placement.OriginalPath = false
		msg := "Original folder path is no longer available; restored to the project root."
		placement.Banner = &msg
	}
	return placement, nil
}

func (r *Repo) bindingRestoreProject(ctx context.Context, tx pgx.Tx, resourceID uuid.UUID) (*uuid.UUID, error) {
	var projectID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT project_id
		   FROM ontology_project_resources
		  WHERE resource_id = $1 AND is_deleted = TRUE`,
		resourceID,
	).Scan(&projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &projectID, nil
}

// scanTrashEntries reads the trash projection tuples and stamps `kind`
// on each row.
func scanTrashEntries(rows pgxRowsLike, kind string) ([]TrashEntry, error) {
	out := make([]TrashEntry, 0)
	for rows.Next() {
		var (
			id                     uuid.UUID
			projectID              *uuid.UUID
			name                   string
			deletedAt              time.Time
			deletedBy              *uuid.UUID
			retentionDays          int
			purgeAfter             *time.Time
			originalProjectID      *uuid.UUID
			originalParentFolderID *uuid.UUID
			restoreTargetStatus    string
		)
		if err := rows.Scan(
			&id,
			&projectID,
			&name,
			&deletedAt,
			&deletedBy,
			&retentionDays,
			&purgeAfter,
			&originalProjectID,
			&originalParentFolderID,
			&restoreTargetStatus,
		); err != nil {
			return nil, err
		}
		out = append(out, TrashEntry{
			ResourceKind:           kind,
			ResourceID:             id,
			ProjectID:              projectID,
			DisplayName:            name,
			DeletedAt:              deletedAt,
			DeletedBy:              deletedBy,
			RetentionDays:          retentionDays,
			PurgeAfter:             purgeAfter,
			OriginalProjectID:      originalProjectID,
			OriginalParentFolderID: originalParentFolderID,
			RestoreTargetStatus:    restoreTargetStatus,
		})
	}
	return out, rows.Err()
}
