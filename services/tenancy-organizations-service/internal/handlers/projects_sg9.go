// projects_sg9.go: SG.9 — Foundry-style access request workflow.
//
// This extends the SG.6 project access-request row into a request with
// independently routed tasks:
//   - direct project-role tasks route to project owners.
//   - internal/rule-based group membership tasks route to configured
//     group administrators.
//   - external groups create handoff tasks with message/URL.
//   - required marking tasks route to configured marking reviewers.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

type accessWorkflowHTTPError struct {
	status int
	msg    string
}

func (e accessWorkflowHTTPError) Error() string { return e.msg }

func accessWorkflowError(status int, msg string) error {
	return accessWorkflowHTTPError{status: status, msg: msg}
}

func writeAccessWorkflowError(w http.ResponseWriter, err error) {
	var typed accessWorkflowHTTPError
	if errors.As(err, &typed) {
		writeJSONErr(w, typed.status, typed.msg)
		return
	}
	writeJSONErr(w, http.StatusInternalServerError, err.Error())
}

// GetProjectAccessRequestForm handles
// GET /api/v1/projects/{id}/access-request-form.
//
// It intentionally does not require existing project view access: this
// is the form users need when they only have a direct link or the
// Discoverer role.
func (h *ProjectsHandlers) GetProjectAccessRequestForm(w http.ResponseWriter, r *http.Request) {
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
	groups, err := listAccessRequestFormGroups(r.Context(), h, projectID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	markings, err := listProjectRequiredMarkings(r.Context(), h, projectID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ProjectAccessRequestFormResponse{
		ProjectID:           projectID,
		RequesterID:         claims.Sub,
		ProjectOwnerID:      project.OwnerID,
		DefaultRole:         project.DefaultRole,
		Groups:              groups,
		RequiredMarkings:    markings,
		DirectRoleReviewers: []uuid.UUID{project.OwnerID},
	})
}

func (h *ProjectsHandlers) UpsertProjectAccessRequestGroupSetting(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "group_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid group_id")
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
	var body models.UpsertProjectAccessRequestGroupSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	setting, err := upsertProjectAccessRequestGroupSetting(r.Context(), h, projectID, groupID, claims.Sub, &body)
	if err != nil {
		writeAccessWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, setting)
}

func (h *ProjectsHandlers) DeleteProjectAccessRequestGroupSetting(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "group_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid group_id")
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
	if _, err := h.Pool.Exec(r.Context(),
		`DELETE FROM ontology_project_access_group_settings
		 WHERE project_id = $1 AND group_id = $2`,
		projectID, groupID,
	); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectsHandlers) UpsertProjectRequiredMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	markingID, err := uuid.Parse(chi.URLParam(r, "marking_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid marking_id")
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
	var body models.UpsertProjectRequiredMarkingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	marking, err := upsertProjectRequiredMarking(r.Context(), h, projectID, markingID, &body)
	if err != nil {
		writeAccessWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marking)
}

func (h *ProjectsHandlers) DeleteProjectRequiredMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	markingID, err := uuid.Parse(chi.URLParam(r, "marking_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid marking_id")
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
	if _, err := h.Pool.Exec(r.Context(),
		`DELETE FROM ontology_project_required_markings
		 WHERE project_id = $1 AND marking_id = $2`,
		projectID, markingID,
	); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAccessRequestInbox handles GET /api/v1/access-requests/inbox.
func (h *ProjectsHandlers) ListAccessRequestInbox(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !isAllowedAccessRequestStatus(status) {
		writeJSONErr(w, http.StatusBadRequest, "status must be pending, approved, denied, cancelled, changes_requested, action_required, or completed")
		return
	}

	args := make([]any, 0, 3)
	query := `SELECT DISTINCT ` + accessRequestSelectColumns("ar") + `
	          FROM ontology_project_access_requests ar
	          JOIN ontology_project_access_request_tasks t ON t.request_id = ar.id
	          JOIN ontology_projects p ON p.id = ar.project_id
	          WHERE p.is_deleted = FALSE`
	if !claims.HasRole("admin") {
		actorJSON, _ := json.Marshal([]uuid.UUID{claims.Sub})
		args = append(args, string(actorJSON), claims.Sub)
		query += ` AND (
			t.reviewer_user_ids @> $1::jsonb
			OR (t.task_type = 'project_role' AND p.owner_id = $2)
		)`
	}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND ar.status = $%d", len(args))
	}
	query += " ORDER BY ar.created_at DESC LIMIT 200"

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

func createAccessRequestWorkflow(
	ctx context.Context,
	h *ProjectsHandlers,
	project *models.OntologyProject,
	actor uuid.UUID,
	body *models.CreateProjectAccessRequestRequest,
) (*models.OntologyProjectAccessRequest, error) {
	reason := ""
	if body.Reason != nil {
		reason = strings.TrimSpace(*body.Reason)
	}
	if reason == "" {
		return nil, accessWorkflowError(http.StatusBadRequest, "reason is required")
	}
	requestType := models.ProjectAccessRequestTypeProjectAccess
	if body.RequestType != nil && strings.TrimSpace(*body.RequestType) != "" {
		var err error
		requestType, err = normalizeAccessRequestType(*body.RequestType)
		if err != nil {
			return nil, err
		}
	}
	targets := body.RequestedForUserIDs
	if len(targets) == 0 {
		targets = []uuid.UUID{actor}
	}

	tasks, requestedRole, err := buildAccessRequestTasks(ctx, h, project, targets, reason, body)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, accessWorkflowError(http.StatusBadRequest, "at least one project role, group membership, or marking access task is required")
	}
	status := models.ProjectAccessRequestStatusPending
	if allTasksActionRequired(tasks) {
		status = models.ProjectAccessRequestStatusActionRequired
	}

	targetsJSON, err := json.Marshal(targets)
	if err != nil {
		return nil, err
	}
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	reqID := ids.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO ontology_project_access_requests
		   (id, project_id, requested_by, request_type, requested_for_user_ids,
		    requested_role, reason, scope_resource_kind, scope_resource_id, status)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10)`,
		reqID, project.ID, actor, requestType, string(targetsJSON),
		string(requestedRole), reason, body.ScopeResourceKind, body.ScopeResourceID, status,
	); err != nil {
		return nil, err
	}

	for i := range tasks {
		tasks[i].RequestID = reqID
		if err := insertAccessRequestTask(ctx, tx, &tasks[i]); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadProjectAccessRequest(ctx, h, reqID)
}

func decideAccessRequestWorkflow(
	ctx context.Context,
	h *ProjectsHandlers,
	project *models.OntologyProject,
	claims *authmw.Claims,
	requestID uuid.UUID,
	body *models.DecideProjectAccessRequestRequest,
) (*models.OntologyProjectAccessRequest, error) {
	decision := strings.TrimSpace(body.Decision)
	if decision != models.ProjectAccessRequestStatusApproved && decision != models.ProjectAccessRequestStatusDenied {
		return nil, accessWorkflowError(http.StatusBadRequest, "decision must be 'approved' or 'denied'")
	}
	req, err := loadProjectAccessRequest(ctx, h, requestID)
	if err != nil {
		return nil, err
	}
	if req == nil || req.ProjectID != project.ID {
		return nil, accessWorkflowError(http.StatusConflict, "request not found or not pending")
	}
	if req.Status == models.ProjectAccessRequestStatusCancelled ||
		req.Status == models.ProjectAccessRequestStatusDenied ||
		req.Status == models.ProjectAccessRequestStatusCompleted {
		return nil, accessWorkflowError(http.StatusConflict, "request not found or not pending")
	}

	// Legacy SG.6 rows had no task table. Keep approving them useful:
	// project owners/admins can materialise the requested project role.
	if len(req.Tasks) == 0 {
		if !claims.HasRole("admin") && project.OwnerID != claims.Sub {
			return nil, accessWorkflowError(http.StatusForbidden, "forbidden: no eligible access-request tasks for current reviewer")
		}
		now := time.Now().UTC()
		status := decision
		if decision == models.ProjectAccessRequestStatusApproved {
			status = models.ProjectAccessRequestStatusCompleted
			if _, err := h.Pool.Exec(ctx,
				`INSERT INTO ontology_project_memberships (project_id, user_id, role)
				 VALUES ($1, $2, $3)
				 ON CONFLICT (project_id, user_id) DO UPDATE
				   SET role = EXCLUDED.role, updated_at = NOW()`,
				project.ID, req.RequestedBy, string(req.RequestedRole),
			); err != nil {
				return nil, err
			}
		}
		if _, err := h.Pool.Exec(ctx,
			`UPDATE ontology_project_access_requests
			 SET status = $3, decided_by = $4, decision_reason = $5,
			     decided_at = $6, completed_at = CASE WHEN $3 = 'completed' THEN $6 ELSE completed_at END
			 WHERE id = $1 AND project_id = $2`,
			requestID, project.ID, status, claims.Sub, body.Reason, now,
		); err != nil {
			return nil, err
		}
		return loadProjectAccessRequest(ctx, h, requestID)
	}

	now := time.Now().UTC()
	eligibleTaskIDs := make([]uuid.UUID, 0)
	for _, task := range req.Tasks {
		if task.Status != models.ProjectAccessRequestTaskStatusReview {
			continue
		}
		if canReviewAccessRequestTask(project, claims, &task) {
			eligibleTaskIDs = append(eligibleTaskIDs, task.ID)
		}
	}
	if len(eligibleTaskIDs) == 0 {
		return nil, accessWorkflowError(http.StatusForbidden, "forbidden: no eligible access-request tasks for current reviewer")
	}

	taskStatus := models.ProjectAccessRequestTaskStatusApproved
	if decision == models.ProjectAccessRequestStatusDenied {
		taskStatus = models.ProjectAccessRequestTaskStatusRejected
	}
	for _, taskID := range eligibleTaskIDs {
		if _, err := h.Pool.Exec(ctx,
			`UPDATE ontology_project_access_request_tasks
			 SET status = $3, decided_by = $4, decision_reason = $5, decided_at = $6
			 WHERE id = $1 AND request_id = $2 AND status = 'review'`,
			taskID, requestID, taskStatus, claims.Sub, body.Reason, now,
		); err != nil {
			return nil, err
		}
	}
	if err := finalizeAccessRequest(ctx, h, project.ID, requestID, claims.Sub, body.Reason); err != nil {
		return nil, err
	}
	return loadProjectAccessRequest(ctx, h, requestID)
}

func buildAccessRequestTasks(
	ctx context.Context,
	h *ProjectsHandlers,
	project *models.OntologyProject,
	targets []uuid.UUID,
	reason string,
	body *models.CreateProjectAccessRequestRequest,
) ([]models.ProjectAccessRequestTask, models.OntologyProjectRole, error) {
	tasks := make([]models.ProjectAccessRequestTask, 0)
	requestedRole := models.OntologyProjectRoleViewer

	for _, input := range body.ProjectRoleRequests {
		role, err := models.ParseOntologyProjectRole(string(input.Role))
		if err != nil {
			return nil, "", accessWorkflowError(http.StatusBadRequest, err.Error())
		}
		requestedRole = role
		targetList := targets
		if input.TargetUserID != nil {
			targetList = []uuid.UUID{*input.TargetUserID}
		}
		for _, userID := range targetList {
			tasks = append(tasks, newAccessRequestTask(project.ID, models.ProjectAccessRequestTaskProjectRole, userID, role, reason))
		}
	}

	if len(body.ProjectRoleRequests) == 0 &&
		len(body.GroupMembershipRequests) == 0 &&
		len(body.MarkingAccessRequests) == 0 &&
		body.RequestedRole != "" {
		role, err := models.ParseOntologyProjectRole(string(body.RequestedRole))
		if err != nil {
			return nil, "", accessWorkflowError(http.StatusBadRequest, err.Error())
		}
		requestedRole = role
		for _, userID := range targets {
			tasks = append(tasks, newAccessRequestTask(project.ID, models.ProjectAccessRequestTaskProjectRole, userID, role, reason))
		}
	}

	for _, input := range body.GroupMembershipRequests {
		if input.GroupID == uuid.Nil {
			return nil, "", accessWorkflowError(http.StatusBadRequest, "group_id is required for group membership requests")
		}
		setting, err := loadProjectAccessRequestGroupSetting(ctx, h, project.ID, input.GroupID)
		if err != nil {
			return nil, "", err
		}
		role := models.OntologyProjectRole("")
		if input.Role != nil {
			parsed, err := models.ParseOntologyProjectRole(string(*input.Role))
			if err != nil {
				return nil, "", accessWorkflowError(http.StatusBadRequest, err.Error())
			}
			role = parsed
		} else if setting != nil && setting.RequestRole != nil {
			role = *setting.RequestRole
		} else {
			parsed, err := loadProjectGroupMembershipRole(ctx, h, project.ID, input.GroupID)
			if err != nil {
				return nil, "", err
			}
			role = parsed
		}
		if role == "" {
			return nil, "", accessWorkflowError(http.StatusBadRequest, "requested group is not bound to a project role")
		}
		requestedRole = role
		targetList := targets
		if input.TargetUserID != nil {
			targetList = []uuid.UUID{*input.TargetUserID}
		}
		for _, userID := range targetList {
			task := newAccessRequestTask(project.ID, models.ProjectAccessRequestTaskGroupMembership, userID, role, reason)
			task.GroupID = &input.GroupID
			if setting != nil && setting.ExcludedFromRequestForms {
				return nil, "", accessWorkflowError(http.StatusBadRequest, "group is excluded from this project's access request form")
			}
			groupKind := models.ProjectAccessGroupKindInternal
			if setting != nil {
				groupKind = setting.GroupKind
			}
			switch groupKind {
			case models.ProjectAccessGroupKindExternal:
				task.TaskType = models.ProjectAccessRequestTaskExternalGroupHandoff
				task.Status = models.ProjectAccessRequestTaskStatusActionRequired
				if setting != nil {
					task.ExternalRequestMessage = setting.ExternalRequestMessage
					task.ExternalRequestURL = setting.ExternalRequestURL
				}
			case models.ProjectAccessGroupKindInternal, models.ProjectAccessGroupKindRuleBased:
				if setting == nil || len(setting.ReviewerUserIDs) == 0 {
					return nil, "", accessWorkflowError(http.StatusConflict, "internal group access requests require reviewer_user_ids from group administrators")
				}
				task.ReviewerUserIDs = append([]uuid.UUID{}, setting.ReviewerUserIDs...)
			default:
				return nil, "", accessWorkflowError(http.StatusBadRequest, "group_kind must be 'internal', 'external', or 'rule_based'")
			}
			tasks = append(tasks, task)
		}
	}

	for _, input := range body.MarkingAccessRequests {
		if input.MarkingID == uuid.Nil {
			return nil, "", accessWorkflowError(http.StatusBadRequest, "marking_id is required for marking access requests")
		}
		setting, err := loadProjectRequiredMarking(ctx, h, project.ID, input.MarkingID)
		if err != nil {
			return nil, "", err
		}
		reviewers := input.ReviewerUserIDs
		markingName := ""
		if input.MarkingName != nil {
			markingName = strings.TrimSpace(*input.MarkingName)
		}
		if setting != nil {
			if len(reviewers) == 0 {
				reviewers = setting.ReviewerUserIDs
			}
			if markingName == "" {
				markingName = setting.MarkingName
			}
		}
		if len(reviewers) == 0 {
			return nil, "", accessWorkflowError(http.StatusConflict, "marking access requests require reviewer_user_ids from marking administrators")
		}
		taskReason := reason
		if input.Reason != nil && strings.TrimSpace(*input.Reason) != "" {
			taskReason = strings.TrimSpace(*input.Reason)
		}
		targetList := targets
		if input.TargetUserID != nil {
			targetList = []uuid.UUID{*input.TargetUserID}
		}
		for _, userID := range targetList {
			task := newAccessRequestTask(project.ID, models.ProjectAccessRequestTaskMarkingAccess, userID, models.OntologyProjectRoleViewer, taskReason)
			task.RequestedRole = nil
			task.MarkingID = &input.MarkingID
			if markingName != "" {
				task.MarkingName = &markingName
			}
			task.ReviewerUserIDs = append([]uuid.UUID{}, reviewers...)
			tasks = append(tasks, task)
		}
	}
	return tasks, requestedRole, nil
}

func newAccessRequestTask(projectID uuid.UUID, taskType string, targetUserID uuid.UUID, role models.OntologyProjectRole, reason string) models.ProjectAccessRequestTask {
	return models.ProjectAccessRequestTask{
		ID:              ids.New(),
		ProjectID:       projectID,
		TaskType:        taskType,
		TargetUserID:    targetUserID,
		RequestedRole:   &role,
		Reason:          reason,
		Status:          models.ProjectAccessRequestTaskStatusReview,
		ReviewerUserIDs: []uuid.UUID{},
	}
}

func insertAccessRequestTask(ctx context.Context, tx pgx.Tx, task *models.ProjectAccessRequestTask) error {
	reviewersJSON, err := json.Marshal(task.ReviewerUserIDs)
	if err != nil {
		return err
	}
	var role *string
	if task.RequestedRole != nil {
		s := string(*task.RequestedRole)
		role = &s
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO ontology_project_access_request_tasks
		   (id, request_id, project_id, task_type, target_user_id, requested_role,
		    group_id, marking_id, marking_name, reason, status, reviewer_user_ids,
		    external_request_message, external_request_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14)`,
		task.ID, task.RequestID, task.ProjectID, task.TaskType, task.TargetUserID, role,
		task.GroupID, task.MarkingID, task.MarkingName, task.Reason, task.Status,
		string(reviewersJSON), task.ExternalRequestMessage, task.ExternalRequestURL,
	)
	return err
}

func finalizeAccessRequest(ctx context.Context, h *ProjectsHandlers, projectID, requestID, actor uuid.UUID, reason *string) error {
	tasks, err := loadProjectAccessRequestTasks(ctx, h, requestID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, task := range tasks {
		if task.Status != models.ProjectAccessRequestTaskStatusApproved {
			continue
		}
		if task.TaskType != models.ProjectAccessRequestTaskProjectRole || task.RequestedRole == nil {
			continue
		}
		if _, err := h.Pool.Exec(ctx,
			`INSERT INTO ontology_project_memberships (project_id, user_id, role)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (project_id, user_id) DO UPDATE
			   SET role = EXCLUDED.role, updated_at = NOW()`,
			projectID, task.TargetUserID, string(*task.RequestedRole),
		); err != nil {
			return err
		}
		if _, err := h.Pool.Exec(ctx,
			`UPDATE ontology_project_access_request_tasks
			 SET status = 'completed', invoked_at = $3
			 WHERE id = $1 AND request_id = $2`,
			task.ID, requestID, now,
		); err != nil {
			return err
		}
	}
	tasks, err = loadProjectAccessRequestTasks(ctx, h, requestID)
	if err != nil {
		return err
	}
	status := summarizeAccessRequestTaskStatuses(tasks)
	var completedAt *time.Time
	if status == models.ProjectAccessRequestStatusCompleted {
		completedAt = &now
	}
	if _, err := h.Pool.Exec(ctx,
		`UPDATE ontology_project_access_requests
		 SET status = $3,
		     decided_by = CASE WHEN $3 IN ('approved','denied','completed') THEN $4 ELSE decided_by END,
		     decision_reason = CASE WHEN $3 IN ('approved','denied','completed') THEN $5 ELSE decision_reason END,
		     decided_at = CASE WHEN $3 IN ('approved','denied','completed') THEN $6 ELSE decided_at END,
		     completed_at = COALESCE($7, completed_at)
		 WHERE id = $1 AND project_id = $2`,
		requestID, projectID, status, actor, reason, now, completedAt,
	); err != nil {
		return err
	}
	return nil
}

func summarizeAccessRequestTaskStatuses(tasks []models.ProjectAccessRequestTask) string {
	if len(tasks) == 0 {
		return models.ProjectAccessRequestStatusPending
	}
	allCompleted := true
	allApprovedOrCompleted := true
	anyActionRequired := false
	for _, task := range tasks {
		switch task.Status {
		case models.ProjectAccessRequestTaskStatusRejected:
			return models.ProjectAccessRequestStatusDenied
		case models.ProjectAccessRequestTaskStatusReview:
			return models.ProjectAccessRequestStatusPending
		case models.ProjectAccessRequestTaskStatusActionRequired:
			anyActionRequired = true
			allCompleted = false
			allApprovedOrCompleted = false
		case models.ProjectAccessRequestTaskStatusApproved:
			allCompleted = false
		case models.ProjectAccessRequestTaskStatusCompleted:
			// ok
		default:
			allCompleted = false
			allApprovedOrCompleted = false
		}
	}
	if anyActionRequired {
		return models.ProjectAccessRequestStatusActionRequired
	}
	if allCompleted {
		return models.ProjectAccessRequestStatusCompleted
	}
	if allApprovedOrCompleted {
		return models.ProjectAccessRequestStatusApproved
	}
	return models.ProjectAccessRequestStatusPending
}

func canReviewAccessRequestTask(project *models.OntologyProject, claims *authmw.Claims, task *models.ProjectAccessRequestTask) bool {
	if claims.HasRole("admin") {
		return true
	}
	switch task.TaskType {
	case models.ProjectAccessRequestTaskProjectRole:
		return project.OwnerID == claims.Sub
	case models.ProjectAccessRequestTaskGroupMembership, models.ProjectAccessRequestTaskMarkingAccess:
		return containsUUID(task.ReviewerUserIDs, claims.Sub)
	default:
		return false
	}
}

func allTasksActionRequired(tasks []models.ProjectAccessRequestTask) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		if task.Status != models.ProjectAccessRequestTaskStatusActionRequired {
			return false
		}
	}
	return true
}

func normalizeAccessRequestType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case models.ProjectAccessRequestTypeProjectAccess:
		return models.ProjectAccessRequestTypeProjectAccess, nil
	case models.ProjectAccessRequestTypeAdditionalProjectAccess:
		return models.ProjectAccessRequestTypeAdditionalProjectAccess, nil
	default:
		return "", accessWorkflowError(http.StatusBadRequest, "request_type must be 'project_access' or 'additional_project_access'")
	}
}

func normalizeProjectAccessGroupKind(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", models.ProjectAccessGroupKindInternal:
		return models.ProjectAccessGroupKindInternal, nil
	case models.ProjectAccessGroupKindExternal:
		return models.ProjectAccessGroupKindExternal, nil
	case models.ProjectAccessGroupKindRuleBased:
		return models.ProjectAccessGroupKindRuleBased, nil
	default:
		return "", accessWorkflowError(http.StatusBadRequest, "group_kind must be 'internal', 'external', or 'rule_based'")
	}
}

func validateExternalURL(raw *string) error {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(*raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return accessWorkflowError(http.StatusBadRequest, "external_request_url must be an absolute URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return accessWorkflowError(http.StatusBadRequest, "external_request_url must use http or https")
	}
	return nil
}

func containsUUID(values []uuid.UUID, needle uuid.UUID) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func accessRequestSelectColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return prefix + `id, ` +
		prefix + `project_id, ` +
		prefix + `requested_by, ` +
		prefix + `request_type, ` +
		prefix + `requested_for_user_ids, ` +
		prefix + `requested_role, ` +
		prefix + `reason, ` +
		prefix + `scope_resource_kind, ` +
		prefix + `scope_resource_id, ` +
		prefix + `status, ` +
		prefix + `decided_by, ` +
		prefix + `decision_reason, ` +
		prefix + `created_at, ` +
		prefix + `decided_at, ` +
		prefix + `completed_at`
}

func attachAccessRequestTasks(ctx context.Context, h *ProjectsHandlers, req *models.OntologyProjectAccessRequest) error {
	tasks, err := loadProjectAccessRequestTasks(ctx, h, req.ID)
	if err != nil {
		return err
	}
	req.Tasks = tasks
	return nil
}

func loadProjectAccessRequestTasks(ctx context.Context, h *ProjectsHandlers, requestID uuid.UUID) ([]models.ProjectAccessRequestTask, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT id, request_id, project_id, task_type, target_user_id, requested_role,
		        group_id, marking_id, marking_name, reason, status, reviewer_user_ids,
		        external_request_message, external_request_url, decided_by, decision_reason,
		        created_at, decided_at, invoked_at
		 FROM ontology_project_access_request_tasks
		 WHERE request_id = $1
		 ORDER BY created_at ASC, id ASC`,
		requestID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ProjectAccessRequestTask, 0)
	for rows.Next() {
		t, err := scanAccessRequestTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func scanAccessRequestTask(row interface{ Scan(dest ...any) error }) (*models.ProjectAccessRequestTask, error) {
	task := &models.ProjectAccessRequestTask{}
	var role *string
	var reviewersRaw []byte
	if err := row.Scan(
		&task.ID, &task.RequestID, &task.ProjectID, &task.TaskType, &task.TargetUserID, &role,
		&task.GroupID, &task.MarkingID, &task.MarkingName, &task.Reason, &task.Status, &reviewersRaw,
		&task.ExternalRequestMessage, &task.ExternalRequestURL, &task.DecidedBy, &task.DecisionReason,
		&task.CreatedAt, &task.DecidedAt, &task.InvokedAt,
	); err != nil {
		return nil, err
	}
	if role != nil {
		parsed := models.OntologyProjectRole(*role)
		task.RequestedRole = &parsed
	}
	reviewers, err := uuidListFromJSON(reviewersRaw)
	if err != nil {
		return nil, fmt.Errorf("decode reviewer_user_ids: %w", err)
	}
	task.ReviewerUserIDs = reviewers
	return task, nil
}

func loadProjectAccessRequestGroupSetting(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, groupID uuid.UUID,
) (*models.ProjectAccessRequestGroupSetting, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT project_id, group_id, group_display_name, group_kind, request_role,
		        reviewer_user_ids, custom_form, external_request_message,
		        external_request_url, excluded_from_request_forms, updated_by,
		        created_at, updated_at
		 FROM ontology_project_access_group_settings
		 WHERE project_id = $1 AND group_id = $2`,
		projectID, groupID,
	)
	setting, err := scanProjectAccessRequestGroupSetting(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return setting, err
}

func upsertProjectAccessRequestGroupSetting(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, groupID, actor uuid.UUID,
	body *models.UpsertProjectAccessRequestGroupSettingRequest,
) (*models.ProjectAccessRequestGroupSetting, error) {
	groupKind := models.ProjectAccessGroupKindInternal
	if body.GroupKind != nil {
		var err error
		groupKind, err = normalizeProjectAccessGroupKind(*body.GroupKind)
		if err != nil {
			return nil, err
		}
	}
	var requestRole *string
	if body.RequestRole != nil {
		parsed, err := models.ParseOntologyProjectRole(string(*body.RequestRole))
		if err != nil {
			return nil, accessWorkflowError(http.StatusBadRequest, err.Error())
		}
		s := string(parsed)
		requestRole = &s
	}
	if err := validateExternalURL(body.ExternalRequestURL); err != nil {
		return nil, err
	}
	customForm := body.CustomForm
	if customForm == nil {
		customForm = map[string]any{}
	}
	customFormJSON, err := json.Marshal(customForm)
	if err != nil {
		return nil, accessWorkflowError(http.StatusBadRequest, "custom_form must be a JSON object")
	}
	reviewersJSON, err := json.Marshal(body.ReviewerUserIDs)
	if err != nil {
		return nil, err
	}
	excluded := false
	if body.ExcludedFromRequestForms != nil {
		excluded = *body.ExcludedFromRequestForms
	}
	if groupKind != models.ProjectAccessGroupKindExternal && !excluded && len(body.ReviewerUserIDs) == 0 {
		return nil, accessWorkflowError(http.StatusBadRequest, "reviewer_user_ids is required for visible internal or rule_based groups")
	}
	row := h.Pool.QueryRow(ctx,
		`INSERT INTO ontology_project_access_group_settings
		   (project_id, group_id, group_display_name, group_kind, request_role,
		    reviewer_user_ids, custom_form, external_request_message,
		    external_request_url, excluded_from_request_forms, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		 ON CONFLICT (project_id, group_id) DO UPDATE
		   SET group_display_name = EXCLUDED.group_display_name,
		       group_kind = EXCLUDED.group_kind,
		       request_role = EXCLUDED.request_role,
		       reviewer_user_ids = EXCLUDED.reviewer_user_ids,
		       custom_form = EXCLUDED.custom_form,
		       external_request_message = EXCLUDED.external_request_message,
		       external_request_url = EXCLUDED.external_request_url,
		       excluded_from_request_forms = EXCLUDED.excluded_from_request_forms,
		       updated_by = EXCLUDED.updated_by,
		       updated_at = NOW()
		 RETURNING project_id, group_id, group_display_name, group_kind, request_role,
		           reviewer_user_ids, custom_form, external_request_message,
		           external_request_url, excluded_from_request_forms, updated_by,
		           created_at, updated_at`,
		projectID, groupID, body.GroupDisplayName, groupKind, requestRole,
		string(reviewersJSON), string(customFormJSON), body.ExternalRequestMessage,
		body.ExternalRequestURL, excluded, actor,
	)
	return scanProjectAccessRequestGroupSetting(row)
}

func scanProjectAccessRequestGroupSetting(row interface{ Scan(dest ...any) error }) (*models.ProjectAccessRequestGroupSetting, error) {
	setting := &models.ProjectAccessRequestGroupSetting{}
	var role *string
	var reviewersRaw []byte
	var customRaw []byte
	if err := row.Scan(
		&setting.ProjectID, &setting.GroupID, &setting.GroupDisplayName, &setting.GroupKind, &role,
		&reviewersRaw, &customRaw, &setting.ExternalRequestMessage, &setting.ExternalRequestURL,
		&setting.ExcludedFromRequestForms, &setting.UpdatedBy, &setting.CreatedAt, &setting.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if role != nil {
		parsed := models.OntologyProjectRole(*role)
		setting.RequestRole = &parsed
	}
	reviewers, err := uuidListFromJSON(reviewersRaw)
	if err != nil {
		return nil, fmt.Errorf("decode reviewer_user_ids: %w", err)
	}
	setting.ReviewerUserIDs = reviewers
	if len(customRaw) == 0 {
		setting.CustomForm = map[string]any{}
	} else if err := json.Unmarshal(customRaw, &setting.CustomForm); err != nil {
		return nil, fmt.Errorf("decode custom_form: %w", err)
	}
	if setting.CustomForm == nil {
		setting.CustomForm = map[string]any{}
	}
	return setting, nil
}

func listAccessRequestFormGroups(ctx context.Context, h *ProjectsHandlers, projectID uuid.UUID) ([]models.ProjectAccessRequestFormGroup, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT gm.group_id,
		        COALESCE(s.request_role, gm.role) AS role,
		        s.group_display_name,
		        COALESCE(s.group_kind, 'internal') AS group_kind,
		        COALESCE(s.reviewer_user_ids, '[]'::jsonb) AS reviewer_user_ids,
		        COALESCE(s.custom_form, '{}'::jsonb) AS custom_form,
		        s.external_request_message,
		        s.external_request_url
		 FROM ontology_project_group_memberships gm
		 LEFT JOIN ontology_project_access_group_settings s
		   ON s.project_id = gm.project_id AND s.group_id = gm.group_id
		 WHERE gm.project_id = $1
		   AND COALESCE(s.excluded_from_request_forms, FALSE) = FALSE
		 ORDER BY role DESC, gm.created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ProjectAccessRequestFormGroup, 0)
	for rows.Next() {
		var item models.ProjectAccessRequestFormGroup
		var role string
		var reviewersRaw []byte
		var customRaw []byte
		if err := rows.Scan(
			&item.GroupID, &role, &item.GroupDisplayName, &item.GroupKind,
			&reviewersRaw, &customRaw, &item.ExternalRequestMessage, &item.ExternalRequestURL,
		); err != nil {
			return nil, err
		}
		item.Role = models.OntologyProjectRole(role)
		reviewers, err := uuidListFromJSON(reviewersRaw)
		if err != nil {
			return nil, err
		}
		item.ReviewerUserIDs = reviewers
		if len(customRaw) == 0 {
			item.CustomForm = map[string]any{}
		} else if err := json.Unmarshal(customRaw, &item.CustomForm); err != nil {
			return nil, err
		}
		if item.CustomForm == nil {
			item.CustomForm = map[string]any{}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func loadProjectGroupMembershipRole(ctx context.Context, h *ProjectsHandlers, projectID, groupID uuid.UUID) (models.OntologyProjectRole, error) {
	var role string
	err := h.Pool.QueryRow(ctx,
		`SELECT role FROM ontology_project_group_memberships
		 WHERE project_id = $1 AND group_id = $2`,
		projectID, groupID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return models.OntologyProjectRole(role), nil
}

func loadProjectRequiredMarking(ctx context.Context, h *ProjectsHandlers, projectID, markingID uuid.UUID) (*models.ProjectRequiredMarking, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT project_id, marking_id, marking_name, reason_prompt,
		        reviewer_user_ids, created_at, updated_at
		 FROM ontology_project_required_markings
		 WHERE project_id = $1 AND marking_id = $2`,
		projectID, markingID,
	)
	marking, err := scanProjectRequiredMarking(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return marking, err
}

func listProjectRequiredMarkings(ctx context.Context, h *ProjectsHandlers, projectID uuid.UUID) ([]models.ProjectRequiredMarking, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT project_id, marking_id, marking_name, reason_prompt,
		        reviewer_user_ids, created_at, updated_at
		 FROM ontology_project_required_markings
		 WHERE project_id = $1
		 ORDER BY marking_name ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ProjectRequiredMarking, 0)
	for rows.Next() {
		marking, err := scanProjectRequiredMarking(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *marking)
	}
	return out, rows.Err()
}

func upsertProjectRequiredMarking(
	ctx context.Context,
	h *ProjectsHandlers,
	projectID, markingID uuid.UUID,
	body *models.UpsertProjectRequiredMarkingRequest,
) (*models.ProjectRequiredMarking, error) {
	if strings.TrimSpace(body.MarkingName) == "" {
		return nil, accessWorkflowError(http.StatusBadRequest, "marking_name is required")
	}
	if len(body.ReviewerUserIDs) == 0 {
		return nil, accessWorkflowError(http.StatusBadRequest, "reviewer_user_ids is required for required markings")
	}
	reviewersJSON, err := json.Marshal(body.ReviewerUserIDs)
	if err != nil {
		return nil, err
	}
	row := h.Pool.QueryRow(ctx,
		`INSERT INTO ontology_project_required_markings
		   (project_id, marking_id, marking_name, reason_prompt, reviewer_user_ids)
		 VALUES ($1, $2, $3, $4, $5::jsonb)
		 ON CONFLICT (project_id, marking_id) DO UPDATE
		   SET marking_name = EXCLUDED.marking_name,
		       reason_prompt = EXCLUDED.reason_prompt,
		       reviewer_user_ids = EXCLUDED.reviewer_user_ids,
		       updated_at = NOW()
		 RETURNING project_id, marking_id, marking_name, reason_prompt,
		           reviewer_user_ids, created_at, updated_at`,
		projectID, markingID, strings.TrimSpace(body.MarkingName), body.ReasonPrompt, string(reviewersJSON),
	)
	return scanProjectRequiredMarking(row)
}

func scanProjectRequiredMarking(row interface{ Scan(dest ...any) error }) (*models.ProjectRequiredMarking, error) {
	marking := &models.ProjectRequiredMarking{}
	var reviewersRaw []byte
	if err := row.Scan(
		&marking.ProjectID, &marking.MarkingID, &marking.MarkingName, &marking.ReasonPrompt,
		&reviewersRaw, &marking.CreatedAt, &marking.UpdatedAt,
	); err != nil {
		return nil, err
	}
	reviewers, err := uuidListFromJSON(reviewersRaw)
	if err != nil {
		return nil, fmt.Errorf("decode reviewer_user_ids: %w", err)
	}
	marking.ReviewerUserIDs = reviewers
	return marking, nil
}

func uuidListFromJSON(raw []byte) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return []uuid.UUID{}, nil
	}
	var values []uuid.UUID
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []uuid.UUID{}
	}
	return values, nil
}
