// Package handlers ports the `services/workflow-automation-service/src/handlers/`
// package: workflows CRUD + run trigger + approvals continuation.
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/domain/lineage"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/state"
)

// CrudHandlers wires the AppState into the workflows CRUD endpoints.
type CrudHandlers struct {
	State *state.AppState
}

// NewCrudHandlers wires the workflows CRUD handlers.
func NewCrudHandlers(s *state.AppState) *CrudHandlers { return &CrudHandlers{State: s} }

// ListWorkflows ports `handlers::crud::list_workflows`.
func (h *CrudHandlers) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)
	page := derefOrInt64(q.Page, 1)
	if page < 1 {
		page = 1
	}
	perPage := clampInt64(derefOrInt64(q.PerPage, 20), 1, 100)
	offset := (page - 1) * perPage

	var searchPattern *string
	if q.Search != nil && *q.Search != "" {
		s := "%" + *q.Search + "%"
		searchPattern = &s
	}

	rows, err := h.State.DB.Query(r.Context(),
		`SELECT `+workflowColumns+` FROM workflows
		   WHERE ($1::TEXT IS NULL OR name ILIKE $1 OR description ILIKE $1)
		     AND ($2::TEXT IS NULL OR trigger_type = $2)
		     AND ($3::TEXT IS NULL OR status = $3)
		   ORDER BY updated_at DESC
		   LIMIT $4 OFFSET $5`,
		searchPattern, q.TriggerType, q.Status, perPage, offset,
	)
	if err != nil {
		slog.Error("list workflows failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := make([]models.WorkflowDefinition, 0)
	for rows.Next() {
		def, err := scanWorkflow(rows)
		if err != nil {
			slog.Error("list workflows scan failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		out = append(out, def)
	}
	if err := rows.Err(); err != nil {
		slog.Error("list workflows rows.Err", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var total int64
	if err := h.State.DB.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM workflows
		   WHERE ($1::TEXT IS NULL OR name ILIKE $1 OR description ILIKE $1)
		     AND ($2::TEXT IS NULL OR trigger_type = $2)
		     AND ($3::TEXT IS NULL OR status = $3)`,
		searchPattern, q.TriggerType, q.Status,
	).Scan(&total); err != nil {
		total = 0
	}

	totalPages := int64(0)
	if perPage > 0 {
		totalPages = int64(math.Ceil(float64(total) / float64(perPage)))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":        out,
		"page":        page,
		"per_page":    perPage,
		"total":       total,
		"total_pages": totalPages,
	})
}

// CreateWorkflow ports `handlers::crud::create_workflow`.
func (h *CrudHandlers) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok || claims == nil {
		writeError(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body models.CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stepsJSON, err := json.Marshal(body.Steps)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	desc := derefStr(body.Description, "")
	status := derefStr(body.Status, "draft")
	webhookSecret := extractStringField(body.TriggerConfig, "secret")
	row := h.State.DB.QueryRow(r.Context(),
		`INSERT INTO workflows
		      (id, name, description, owner_id, status, trigger_type,
		       trigger_config, steps, webhook_secret, next_run_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
		    RETURNING `+workflowColumns,
		uuid.New(), body.Name, desc, claims.Sub, status, body.TriggerType,
		ensureJSONObject(body.TriggerConfig), stepsJSON, webhookSecret,
	)
	def, err := scanWorkflow(row)
	if err != nil {
		slog.Error("create workflow failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	syncWorkflowLineageBestEffort(r.Context(), h.State, &def)
	writeJSON(w, http.StatusCreated, def)
}

// GetWorkflow ports `handlers::crud::get_workflow`.
func (h *CrudHandlers) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	def, err := LoadWorkflow(r.Context(), h.State.DB, id)
	if err != nil {
		slog.Error("get workflow failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if def == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, def)
}

// UpdateWorkflow ports `handlers::crud::update_workflow`.
func (h *CrudHandlers) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var body models.UpdateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := LoadWorkflow(r.Context(), h.State.DB, id)
	if err != nil {
		slog.Error("load workflow for update failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if existing == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	nextName := derefStrPtr(body.Name, existing.Name)
	nextDescription := derefStrPtr(body.Description, existing.Description)
	nextStatus := derefStrPtr(body.Status, existing.Status)
	nextTriggerType := derefStrPtr(body.TriggerType, existing.TriggerType)
	nextTriggerConfig := existing.TriggerConfig
	if body.TriggerConfig != nil {
		nextTriggerConfig = ensureJSONObject(*body.TriggerConfig)
	}
	nextSteps := existing.Steps
	if body.Steps != nil {
		raw, err := json.Marshal(*body.Steps)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow steps")
			return
		}
		nextSteps = raw
	}
	webhookSecret := extractStringField(nextTriggerConfig, "secret")

	row := h.State.DB.QueryRow(r.Context(),
		`UPDATE workflows
		    SET name = $2, description = $3, status = $4, trigger_type = $5,
		        trigger_config = $6, steps = $7, webhook_secret = $8,
		        next_run_at = NULL, updated_at = NOW()
		  WHERE id = $1
		  RETURNING `+workflowColumns,
		id, nextName, nextDescription, nextStatus, nextTriggerType,
		nextTriggerConfig, nextSteps, webhookSecret,
	)
	def, err := scanWorkflow(row)
	if err != nil {
		slog.Error("update workflow failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	syncWorkflowLineageBestEffort(r.Context(), h.State, &def)
	writeJSON(w, http.StatusOK, def)
}

// DeleteWorkflow ports `handlers::crud::delete_workflow`.
func (h *CrudHandlers) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	tag, err := h.State.DB.Exec(r.Context(), `DELETE FROM workflows WHERE id = $1`, id)
	if err != nil {
		slog.Error("delete workflow failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := lineage.DeleteWorkflow(r.Context(), h.State.HTTPClient, h.State.PipelineServiceURL, id); err != nil {
		slog.Warn("workflow lineage delete failed",
			slog.String("workflow_id", id.String()), slog.String("error", err.Error()))
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseListQuery(r *http.Request) models.ListWorkflowsQuery {
	q := models.ListWorkflowsQuery{}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.Page = &n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.PerPage = &n
		}
	}
	if v := r.URL.Query().Get("search"); v != "" {
		q.Search = &v
	}
	if v := r.URL.Query().Get("trigger_type"); v != "" {
		q.TriggerType = &v
	}
	if v := r.URL.Query().Get("status"); v != "" {
		q.Status = &v
	}
	return q
}

func syncWorkflowLineageBestEffort(ctx context.Context, s *state.AppState, def *models.WorkflowDefinition) {
	if err := lineage.SyncWorkflow(ctx, s.HTTPClient, s.PipelineServiceURL, def); err != nil {
		slog.Warn("workflow lineage sync failed",
			slog.String("workflow_id", def.ID.String()), slog.String("error", err.Error()))
	}
}

func extractStringField(raw json.RawMessage, key string) *string {
	if len(raw) == 0 {
		return nil
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return nil
	}
	v, ok := holder[key].(string)
	if !ok || v == "" {
		return nil
	}
	out := v
	return &out
}

func ensureJSONObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var holder any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return json.RawMessage(`{}`)
	}
	if _, ok := holder.(map[string]any); !ok {
		return json.RawMessage(`{}`)
	}
	return raw
}

func derefStr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func derefStrPtr(p *string, fallback string) string { return derefStr(p, fallback) }

func derefOrInt64(p *int64, fallback int64) int64 {
	if p == nil {
		return fallback
	}
	return *p
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
