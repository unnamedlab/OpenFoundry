package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

// ListRuns ports `handlers::runs::list_runs`.
func (h *CrudHandlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	workflowID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
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
	perPage = clampInt64(perPage, 1, 100)
	offset := (page - 1) * perPage

	rows, err := h.State.DB.Query(r.Context(),
		`SELECT id, workflow_id, trigger_type, status, started_by, current_step_id,
		        context, error_message, started_at, finished_at
		   FROM workflow_run_projections
		  WHERE workflow_id = $1
		  ORDER BY started_at DESC
		  LIMIT $2 OFFSET $3`,
		workflowID, perPage, offset,
	)
	if err != nil {
		slog.Error("list workflow runs failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := make([]models.WorkflowRun, 0)
	for rows.Next() {
		var run models.WorkflowRun
		var context, errMsg []byte
		if err := rows.Scan(
			&run.ID, &run.WorkflowID, &run.TriggerType, &run.Status, &run.StartedBy,
			&run.CurrentStepID, &context, &errMsg, &run.StartedAt, &run.FinishedAt,
		); err != nil {
			slog.Error("scan workflow run failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		run.Context = context
		if len(errMsg) > 0 {
			s := string(errMsg)
			run.ErrorMessage = &s
		}
		out = append(out, run)
	}

	var total int64
	if err := h.State.DB.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM workflow_run_projections WHERE workflow_id = $1`,
		workflowID,
	).Scan(&total); err != nil {
		total = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":     out,
		"page":     page,
		"per_page": perPage,
		"total":    total,
	})
}
