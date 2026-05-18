package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

const (
	viewReqJobStatusPending   = "pending"
	viewReqJobStatusRunning   = "running"
	viewReqJobStatusSucceeded = "succeeded"
	viewReqJobStatusFailed    = "failed"
	viewReqParentProject      = "project"
	viewReqParentFolder       = "folder"
	viewReqAuditSource        = "tenancy-organizations-service"
	viewReqAuditDependentCap  = 50
)

type propagationTarget struct {
	Kind             string
	ResourceKind     string
	ID               uuid.UUID
	RID              string
	Relationship     string
	PreviousMarkings []string
}

// ListProjectPropagationJobs returns recent CMP.19 background jobs so the
// admin UI can show progress after a parent propagation policy change.
func (h *ProjectsHandlers) ListProjectPropagationJobs(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, projectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	limit := int64(10)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 {
			writeJSONErr(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+viewRequirementPropagationJobColumns+`
		   FROM compass_view_requirement_propagation_jobs
		  WHERE project_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		projectID, limit,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list propagation jobs: %s", err))
		return
	}
	jobs, err := scanViewRequirementPropagationJobs(rows)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to list propagation jobs: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, models.ListViewRequirementPropagationJobsResponse{Data: jobs})
}

// GetProjectPropagationJob returns one CMP.19 background job by id.
func (h *ProjectsHandlers) GetProjectPropagationJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid job_id")
		return
	}
	if _, err := domain.EnsureProjectViewAccess(r.Context(), h.Pool, claims, projectID); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	job, err := loadViewRequirementPropagationJob(r.Context(), h.Pool, projectID, jobID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to load propagation job: %s", err))
		return
	}
	if job == nil {
		writeJSONErr(w, http.StatusNotFound, "propagation job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

const viewRequirementPropagationJobColumns = `id, project_id, parent_resource_kind, parent_resource_id,
        parent_resource_rid, initiated_by, status,
        COALESCE(target_marking_rids, '[]'::jsonb),
        COALESCE(previous_marking_rids, '[]'::jsonb),
        total_folders, processed_folders, changed_folders,
        total_resources, processed_resources, changed_resources,
        error_message, created_at, started_at, finished_at, updated_at`

func scanViewRequirementPropagationJobs(rows pgx.Rows) ([]models.ViewRequirementPropagationJob, error) {
	defer rows.Close()
	out := make([]models.ViewRequirementPropagationJob, 0)
	for rows.Next() {
		job, err := scanViewRequirementPropagationJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanViewRequirementPropagationJobRow(row projectScannable) (*models.ViewRequirementPropagationJob, error) {
	job := &models.ViewRequirementPropagationJob{}
	var targetMarkings []byte
	var previousMarkings []byte
	err := row.Scan(
		&job.ID, &job.ProjectID, &job.ParentResourceKind, &job.ParentResourceID,
		&job.ParentResourceRID, &job.InitiatedBy, &job.Status,
		&targetMarkings, &previousMarkings,
		&job.TotalFolders, &job.ProcessedFolders, &job.ChangedFolders,
		&job.TotalResources, &job.ProcessedResources, &job.ChangedResources,
		&job.ErrorMessage, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.TargetMarkingRIDs = decodeStringSliceJSON(targetMarkings)
	job.PreviousMarkingRIDs = decodeStringSliceJSON(previousMarkings)
	return job, nil
}

func loadViewRequirementPropagationJob(ctx context.Context, q folderQueryRower, projectID, jobID uuid.UUID) (*models.ViewRequirementPropagationJob, error) {
	return scanViewRequirementPropagationJobRow(q.QueryRow(ctx,
		`SELECT `+viewRequirementPropagationJobColumns+`
		   FROM compass_view_requirement_propagation_jobs
		  WHERE project_id = $1 AND id = $2`,
		projectID, jobID,
	))
}

func insertViewRequirementPropagationJobTx(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	parentKind string,
	parentID uuid.UUID,
	parentRID string,
	initiatedBy uuid.UUID,
	targetMarkings []string,
	previousMarkings []string,
) (*models.ViewRequirementPropagationJob, error) {
	targetJSON, err := jsonStringSlice(targetMarkings)
	if err != nil {
		return nil, err
	}
	previousJSON, err := jsonStringSlice(previousMarkings)
	if err != nil {
		return nil, err
	}
	jobID := ids.New()
	row := tx.QueryRow(ctx,
		`INSERT INTO compass_view_requirement_propagation_jobs (
		     id, project_id, parent_resource_kind, parent_resource_id,
		     parent_resource_rid, initiated_by, target_marking_rids, previous_marking_rids
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb)
		 RETURNING `+viewRequirementPropagationJobColumns,
		jobID, projectID, parentKind, parentID, parentRID, initiatedBy, targetJSON, previousJSON,
	)
	return scanViewRequirementPropagationJobRow(row)
}

func (h *ProjectsHandlers) launchViewRequirementPropagationJob(job *models.ViewRequirementPropagationJob) {
	if h == nil || h.Pool == nil || job == nil {
		return
	}
	go func(jobID uuid.UUID) {
		if err := h.runViewRequirementPropagationJob(context.Background(), jobID); err != nil {
			_ = h.markViewRequirementPropagationJobFailed(context.Background(), jobID, err)
		}
	}(job.ID)
}

func (h *ProjectsHandlers) runViewRequirementPropagationJob(ctx context.Context, jobID uuid.UUID) error {
	job, err := h.loadPropagationJobForRun(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil || job.Status == viewReqJobStatusSucceeded || job.Status == viewReqJobStatusFailed {
		return nil
	}
	folders, resources, err := h.loadPropagationTargets(ctx, job)
	if err != nil {
		return err
	}
	if err := h.markViewRequirementPropagationJobRunning(ctx, job.ID, len(folders), len(resources)); err != nil {
		return err
	}

	affected := make([]audittrail.AffectedDependent, 0, viewReqAuditDependentCap)
	changedFolders := 0
	for _, target := range folders {
		changed, err := h.applyFolderPropagationTarget(ctx, job, target)
		if err != nil {
			return err
		}
		if changed {
			changedFolders++
			affected = appendPropagationDependent(affected, target)
		}
	}

	changedResources := 0
	for _, target := range resources {
		changed, err := h.applyResourcePropagationTarget(ctx, job, target)
		if err != nil {
			return err
		}
		if changed {
			changedResources++
			affected = appendPropagationDependent(affected, target)
		}
	}

	return h.completeViewRequirementPropagationJob(ctx, job, changedFolders, changedResources, affected)
}

func (h *ProjectsHandlers) loadPropagationJobForRun(ctx context.Context, jobID uuid.UUID) (*models.ViewRequirementPropagationJob, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT `+viewRequirementPropagationJobColumns+`
		   FROM compass_view_requirement_propagation_jobs
		  WHERE id = $1`,
		jobID,
	)
	return scanViewRequirementPropagationJobRow(row)
}

func (h *ProjectsHandlers) loadPropagationTargets(
	ctx context.Context,
	job *models.ViewRequirementPropagationJob,
) ([]propagationTarget, []propagationTarget, error) {
	var folderRows pgx.Rows
	var err error
	if job.ParentResourceKind == viewReqParentFolder {
		folderRows, err = h.Pool.Query(ctx, propagationFolderTargetsSQL(job.ParentResourceKind), job.ProjectID, job.ParentResourceID)
	} else {
		folderRows, err = h.Pool.Query(ctx, propagationFolderTargetsSQL(job.ParentResourceKind), job.ProjectID)
	}
	if err != nil {
		return nil, nil, err
	}
	folders := make([]propagationTarget, 0)
	for folderRows.Next() {
		var target propagationTarget
		var markings []byte
		if err := folderRows.Scan(&target.ID, &target.RID, &markings); err != nil {
			folderRows.Close()
			return nil, nil, err
		}
		target.Kind = "folder"
		target.Relationship = "view_requirements_child_folder"
		target.PreviousMarkings = decodeStringSliceJSON(markings)
		folders = append(folders, target)
	}
	if err := folderRows.Err(); err != nil {
		folderRows.Close()
		return nil, nil, err
	}
	folderRows.Close()

	resources := make([]propagationTarget, 0)
	if job.ParentResourceKind == viewReqParentProject {
		resourceRows, err := h.Pool.Query(ctx,
			`SELECT resource_kind, resource_id,
			        'ri.compass.main.resource_binding.' || resource_id::text,
			        COALESCE(view_requirement_marking_rids, '[]'::jsonb)
			   FROM ontology_project_resources
			  WHERE project_id = $1 AND is_deleted = FALSE
			  ORDER BY created_at ASC`,
			job.ProjectID,
		)
		if err != nil {
			return nil, nil, err
		}
		for resourceRows.Next() {
			var target propagationTarget
			var resourceKind string
			var markings []byte
			if err := resourceRows.Scan(&resourceKind, &target.ID, &target.RID, &markings); err != nil {
				resourceRows.Close()
				return nil, nil, err
			}
			target.ResourceKind = resourceKind
			target.Kind = "resource_binding:" + resourceKind
			target.Relationship = "view_requirements_project_resource"
			target.PreviousMarkings = decodeStringSliceJSON(markings)
			resources = append(resources, target)
		}
		if err := resourceRows.Err(); err != nil {
			resourceRows.Close()
			return nil, nil, err
		}
		resourceRows.Close()
	}
	return folders, resources, nil
}

func propagationFolderTargetsSQL(parentKind string) string {
	if parentKind == viewReqParentFolder {
		return `WITH RECURSIVE descendants AS (
		            SELECT id, rid, view_requirement_marking_rids
		              FROM ontology_project_folders
		             WHERE project_id = $1 AND parent_folder_id = $2 AND is_deleted = FALSE
		            UNION ALL
		            SELECT f.id, f.rid, f.view_requirement_marking_rids
		              FROM ontology_project_folders f
		              JOIN descendants d ON d.id = f.parent_folder_id
		             WHERE f.project_id = $1 AND f.is_deleted = FALSE
		        )
		        SELECT id, COALESCE(rid, 'ri.compass.main.folder.' || id::text),
		               COALESCE(view_requirement_marking_rids, '[]'::jsonb)
		          FROM descendants
		         ORDER BY id`
	}
	return `SELECT id, COALESCE(rid, 'ri.compass.main.folder.' || id::text),
	               COALESCE(view_requirement_marking_rids, '[]'::jsonb)
	          FROM ontology_project_folders
	         WHERE project_id = $1 AND is_deleted = FALSE
	         ORDER BY created_at ASC`
}

func (h *ProjectsHandlers) markViewRequirementPropagationJobRunning(ctx context.Context, jobID uuid.UUID, totalFolders, totalResources int) error {
	_, err := h.Pool.Exec(ctx,
		`UPDATE compass_view_requirement_propagation_jobs
		    SET status = $2,
		        started_at = COALESCE(started_at, NOW()),
		        total_folders = $3,
		        total_resources = $4,
		        updated_at = NOW()
		  WHERE id = $1 AND status IN ($5, $6)`,
		jobID, viewReqJobStatusRunning, totalFolders, totalResources, viewReqJobStatusPending, viewReqJobStatusRunning,
	)
	return err
}

func (h *ProjectsHandlers) applyFolderPropagationTarget(ctx context.Context, job *models.ViewRequirementPropagationJob, target propagationTarget) (bool, error) {
	changed := !sameStringSlice(target.PreviousMarkings, job.TargetMarkingRIDs)
	markingsJSON, err := jsonStringSlice(job.TargetMarkingRIDs)
	if err != nil {
		return false, err
	}
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	if changed {
		if _, err := tx.Exec(ctx,
			`UPDATE ontology_project_folders
			    SET view_requirement_marking_rids = $2::jsonb,
			        updated_at = NOW()
			  WHERE project_id = $3 AND id = $1 AND is_deleted = FALSE`,
			target.ID, markingsJSON, job.ProjectID,
		); err != nil {
			return false, err
		}
		if err := workspace.UpsertFolderSearchIndexTx(ctx, tx, target.ID, workspace.ResourceSearchEventUpdated); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE compass_view_requirement_propagation_jobs
		    SET processed_folders = processed_folders + 1,
		        changed_folders = changed_folders + $2,
		        updated_at = NOW()
		  WHERE id = $1`,
		job.ID, boolInt(changed),
	); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}

func (h *ProjectsHandlers) applyResourcePropagationTarget(ctx context.Context, job *models.ViewRequirementPropagationJob, target propagationTarget) (bool, error) {
	changed := !sameStringSlice(target.PreviousMarkings, job.TargetMarkingRIDs)
	markingsJSON, err := jsonStringSlice(job.TargetMarkingRIDs)
	if err != nil {
		return false, err
	}
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	if changed {
		if _, err := tx.Exec(ctx,
			`UPDATE ontology_project_resources
			    SET view_requirement_marking_rids = $3::jsonb
			  WHERE project_id = $1 AND resource_id = $2 AND resource_kind = $4 AND is_deleted = FALSE`,
			job.ProjectID, target.ID, markingsJSON, target.ResourceKind,
		); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE compass_view_requirement_propagation_jobs
		    SET processed_resources = processed_resources + 1,
		        changed_resources = changed_resources + $2,
		        updated_at = NOW()
		  WHERE id = $1`,
		job.ID, boolInt(changed),
	); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}

func (h *ProjectsHandlers) completeViewRequirementPropagationJob(
	ctx context.Context,
	job *models.ViewRequirementPropagationJob,
	changedFolders int,
	changedResources int,
	affected []audittrail.AffectedDependent,
) error {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	row := tx.QueryRow(ctx,
		`UPDATE compass_view_requirement_propagation_jobs
		    SET status = $2,
		        finished_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1
		  RETURNING `+viewRequirementPropagationJobColumns,
		job.ID, viewReqJobStatusSucceeded,
	)
	updated, err := scanViewRequirementPropagationJobRow(row)
	if err != nil {
		return err
	}
	truncated := (changedFolders + changedResources) > len(affected)
	event := audittrail.NewCompassViewRequirementsPropagated(
		job.ParentResourceRID,
		models.ProjectRIDFromID(job.ProjectID),
		job.TargetMarkingRIDs,
		job.PreviousMarkingRIDs,
		job.ParentResourceKind,
		job.ID.String(),
		updated.TotalFolders,
		changedFolders,
		updated.TotalResources,
		changedResources,
		affected,
		truncated,
	)
	auditCtx := audittrail.AuditContext{
		ActorID:       job.InitiatedBy.String(),
		RequestID:     job.ID.String(),
		CorrelationID: job.ID.String(),
		SourceService: viewReqAuditSource,
	}
	if err := audittrail.EmitToOutbox(ctx, tx, event, auditCtx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (h *ProjectsHandlers) markViewRequirementPropagationJobFailed(ctx context.Context, jobID uuid.UUID, cause error) error {
	if cause == nil {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`UPDATE compass_view_requirement_propagation_jobs
		    SET status = $2,
		        error_message = $3,
		        finished_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1 AND status <> $4`,
		jobID, viewReqJobStatusFailed, cause.Error(), viewReqJobStatusSucceeded,
	)
	return err
}

func appendPropagationDependent(out []audittrail.AffectedDependent, target propagationTarget) []audittrail.AffectedDependent {
	if len(out) >= viewReqAuditDependentCap {
		return out
	}
	return append(out, audittrail.AffectedDependent{
		Kind:         target.Kind,
		RID:          target.RID,
		ID:           target.ID.String(),
		Relationship: target.Relationship,
		Action:       "view_requirements_updated",
	})
}

func sameStringSlice(left, right []string) bool {
	left = normalizeStringValues(left)
	right = normalizeStringValues(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
