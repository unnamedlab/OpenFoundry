package streamingmonitors

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
)

// PermMonitorWrite is the granular permission key checked alongside
// admin / monitoring_admin / data_engineer roles.
const PermMonitorWrite = "monitoring:write"

// Handlers wires the streaming-monitor HTTP surface.
type Handlers struct{ Repo *Repo }

// canWrite mirrors the Rust can_write helper.
func canWrite(c *authmw.Claims) bool {
	if c == nil {
		return false
	}
	if c.HasAnyRole([]string{"admin", "monitoring_admin", "data_engineer"}) {
		return true
	}
	return c.HasPermissionKey(PermMonitorWrite)
}

// callerSubject returns the JWT sub as a string, or empty when missing.
func callerSubject(r *http.Request) string {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		return ""
	}
	return c.Sub.String()
}

// ─── Monitoring views ───────────────────────────────────────────────

// ListViews handles GET /api/v1/monitoring-views.
func (h *Handlers) ListViews(w http.ResponseWriter, r *http.Request) {
	views, err := h.Repo.ListViews(r.Context())
	if err != nil {
		slog.Error("list views", slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, DataEnvelope[MonitoringView]{Data: views})
}

// CreateView handles POST /api/v1/monitoring-views.
func (h *Handlers) CreateView(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok || !canWrite(c) {
		writeText(w, http.StatusForbidden, PermMonitorWrite+" required")
		return
	}
	var body CreateMonitoringViewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	if trimEmpty(body.Name) || trimEmpty(body.ProjectRID) {
		writeText(w, http.StatusBadRequest, "name and project_rid required")
		return
	}
	desc := ""
	if body.Description != nil {
		desc = *body.Description
	}
	v, err := h.Repo.CreateView(r.Context(), ids.New(), body.Name, desc, body.ProjectRID, c.Sub.String())
	if err != nil {
		slog.Error("create view", slog.String("error", err.Error()))
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// GetView handles GET /api/v1/monitoring-views/{id}.
func (h *Handlers) GetView(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	v, err := h.Repo.GetView(r.Context(), id)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if v == nil {
		writeText(w, http.StatusNotFound, "view not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ─── Monitor rules ──────────────────────────────────────────────────

// ListRulesForView handles GET /api/v1/monitoring-views/{id}/rules.
func (h *Handlers) ListRulesForView(w http.ResponseWriter, r *http.Request) {
	viewID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	rules, err := h.Repo.ListRulesForView(r.Context(), viewID)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, DataEnvelope[MonitorRule]{Data: rules})
}

// CreateRule handles POST /api/v1/monitoring-views/{id}/rules.
//
// The path's view_id MUST match body.view_id (defensive — the path is
// the canonical owner; the body field exists for API symmetry with the
// global POST surface).
func (h *Handlers) CreateRule(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok || !canWrite(c) {
		writeText(w, http.StatusForbidden, PermMonitorWrite+" required")
		return
	}
	viewID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body CreateMonitorRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.ViewID == uuid.Nil {
		body.ViewID = viewID
	} else if body.ViewID != viewID {
		writeText(w, http.StatusBadRequest, "view_id mismatch between path and body")
		return
	}
	if err := body.Validate(); err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := h.Repo.CreateRule(r.Context(), ids.New(), &body, c.Sub.String())
	if err != nil {
		slog.Error("create rule", slog.String("error", err.Error()))
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// ListRules handles GET /api/v1/monitor-rules?resource_type=..&resource_rid=..
//
// Used by streaming-service Job Details to render the "Active monitors" card.
func (h *Handlers) ListRules(w http.ResponseWriter, r *http.Request) {
	q := ListRulesQuery{
		ResourceType: ResourceType(r.URL.Query().Get("resource_type")),
		ResourceRID:  r.URL.Query().Get("resource_rid"),
		MonitorKind:  MonitorKind(r.URL.Query().Get("monitor_kind")),
	}
	if q.ResourceType != "" && !q.ResourceType.IsValid() {
		writeText(w, http.StatusBadRequest, "invalid resource_type")
		return
	}
	if q.MonitorKind != "" && !q.MonitorKind.IsValid() {
		writeText(w, http.StatusBadRequest, "invalid monitor_kind")
		return
	}
	rules, err := h.Repo.ListRules(r.Context(), q)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, DataEnvelope[MonitorRule]{Data: rules})
}

// PatchRule handles PATCH /api/v1/monitor-rules/{id}.
func (h *Handlers) PatchRule(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok || !canWrite(c) {
		writeText(w, http.StatusForbidden, PermMonitorWrite+" required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body PatchRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Comparator != nil && !body.Comparator.IsValid() {
		writeText(w, http.StatusBadRequest, "invalid comparator")
		return
	}
	if body.Severity != nil && !body.Severity.IsValid() {
		writeText(w, http.StatusBadRequest, "invalid severity")
		return
	}
	if body.WindowSeconds != nil && (*body.WindowSeconds < 60 || *body.WindowSeconds > 86_400) {
		writeText(w, http.StatusBadRequest, "window_seconds out of range")
		return
	}
	updated, err := h.Repo.PatchRule(r.Context(), id, &body)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	if updated == nil {
		writeText(w, http.StatusNotFound, "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DeleteRule handles DELETE /api/v1/monitor-rules/{id}.
func (h *Handlers) DeleteRule(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok || !canWrite(c) {
		writeText(w, http.StatusForbidden, PermMonitorWrite+" required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteRule(r.Context(), id)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if !deleted {
		writeText(w, http.StatusNotFound, "rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListEvaluations handles GET /api/v1/monitor-rules/{id}/evaluations?limit=N.
func (h *Handlers) ListEvaluations(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	evals, err := h.Repo.ListEvaluations(r.Context(), id, limit)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, DataEnvelope[MonitorEvaluation]{Data: evals})
}

// ─── helpers ────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}

// callerSubject is reserved for places we still need it (audit fields
// that haven't migrated yet). Keep around to avoid premature deletion.
var _ = callerSubject
