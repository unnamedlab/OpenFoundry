// Package handlers ports `services/lineage-service/src/handlers/lineage.rs`.
//
// Six HTTP handlers + the shared header decorator. Routes are mounted
// in [server.New] when the binary is in HTTP-health mode (the
// Kafka→Iceberg sink defers from a separate runtime).
package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/lineage"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/queryrouter"
)

// Handlers carries the lineage AppState. Constructed once in main and
// passed to the router.
type Handlers struct {
	State *lineage.AppState
}

// NewHandlers wires the lineage AppState.
func NewHandlers(state *lineage.AppState) *Handlers { return &Handlers{State: state} }

func writeJSON(w http.ResponseWriter, status int, body any, plan queryrouter.QueryPlan) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	withQueryHeaders(w, plan)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeStatus(w http.ResponseWriter, status int, plan queryrouter.QueryPlan) {
	withQueryHeaders(w, plan)
	w.WriteHeader(status)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// withQueryHeaders ports `with_query_headers` from Rust verbatim.
func withQueryHeaders(w http.ResponseWriter, plan queryrouter.QueryPlan) {
	if plan.Kind == "" {
		return
	}
	w.Header().Set("x-openfoundry-lineage-scope", plan.Kind.AsScopeLabel())
	w.Header().Set("x-openfoundry-lineage-requested-source", plan.RequestedSource.AsMetricLabel())
	w.Header().Set("x-openfoundry-lineage-served-source", plan.SelectedSource.AsMetricLabel())
	if plan.Degraded {
		w.Header().Set("x-openfoundry-lineage-degraded", "true")
	} else {
		w.Header().Set("x-openfoundry-lineage-degraded", "false")
	}
	w.Header().Set("x-openfoundry-lineage-window-hours", strconv.FormatUint(uint64(plan.WindowHours), 10))
}

func planFor(kind queryrouter.QueryKind, query LineageQueryRequest) queryrouter.QueryPlan {
	envValue := os.Getenv("LINEAGE_TRINO_ENABLED")
	available := queryrouter.TrinoAvailableFromEnv(&envValue)
	return queryrouter.Plan(kind, query.WindowHours, query.Historical, available)
}

func logDegraded(subjectID *uuid.UUID, plan queryrouter.QueryPlan) {
	if !plan.Degraded || !plan.IsHistorical() {
		return
	}
	attrs := []any{
		slog.String("scope", plan.Kind.AsScopeLabel()),
		slog.String("requested_source", plan.RequestedSource.AsMetricLabel()),
		slog.String("served_source", plan.SelectedSource.AsMetricLabel()),
		slog.Uint64("window_hours", uint64(plan.WindowHours)),
	}
	if subjectID != nil {
		attrs = append([]any{slog.String("subject_id", subjectID.String())}, attrs...)
	}
	slog.Info("historical lineage query degraded to Cassandra read-model", attrs...)
}

// LineageQueryRequest mirrors the Rust query struct.
type LineageQueryRequest struct {
	Historical  bool
	WindowHours *uint32
}

func parseQuery(r *http.Request) LineageQueryRequest {
	q := LineageQueryRequest{}
	if v := r.URL.Query().Get("historical"); v != "" {
		q.Historical = strings.EqualFold(v, "true") || v == "1"
	}
	if v := r.URL.Query().Get("window_hours"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			h := uint32(n)
			q.WindowHours = &h
		}
	}
	return q
}

// GetDatasetLineage ports `get_dataset_lineage`.
func (h *Handlers) GetDatasetLineage(w http.ResponseWriter, r *http.Request) {
	claims, _ := authmw.FromContext(r.Context())
	datasetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}

	if _, err := lineage.EnsureDatasetSnapshot(r.Context(), h.State, datasetID); err != nil {
		slog.Warn("dataset snapshot refresh failed", slog.String("dataset_id", datasetID.String()), slog.String("error", err.Error()))
	}

	plan := planFor(queryrouter.KindDatasetGraph, parseQuery(r))
	logDegraded(&datasetID, plan)

	graph, err := lineage.GetLineageGraph(r.Context(), h.State, datasetID, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lineage.FilterGraphForClaims(graph, claims), plan)
}

// GetDatasetColumnLineage ports `get_dataset_column_lineage`.
func (h *Handlers) GetDatasetColumnLineage(w http.ResponseWriter, r *http.Request) {
	datasetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	plan := planFor(queryrouter.KindDatasetColumns, parseQuery(r))
	logDegraded(&datasetID, plan)

	edges, err := lineage.GetDatasetColumnLineage(r.Context(), h.State, datasetID, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, edges, plan)
}

// GetFullLineage ports `get_full_lineage`.
func (h *Handlers) GetFullLineage(w http.ResponseWriter, r *http.Request) {
	claims, _ := authmw.FromContext(r.Context())
	plan := planFor(queryrouter.KindFullGraph, parseQuery(r))
	logDegraded(nil, plan)

	graph, err := lineage.GetFullLineageGraph(r.Context(), h.State, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lineage.FilterGraphForClaims(graph, claims), plan)
}

// GetDatasetLineageImpact ports `get_dataset_lineage_impact`.
func (h *Handlers) GetDatasetLineageImpact(w http.ResponseWriter, r *http.Request) {
	claims, _ := authmw.FromContext(r.Context())
	datasetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}

	if _, err := lineage.EnsureDatasetSnapshot(r.Context(), h.State, datasetID); err != nil {
		slog.Warn("dataset snapshot refresh failed", slog.String("dataset_id", datasetID.String()), slog.String("error", err.Error()))
	}

	plan := planFor(queryrouter.KindDatasetImpact, parseQuery(r))
	logDegraded(&datasetID, plan)

	impact, err := lineage.GetLineageImpactAnalysis(r.Context(), h.State, datasetID, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if impact == nil {
		writeStatus(w, http.StatusNotFound, plan)
		return
	}
	filtered, err := lineage.FilterImpactForClaims(*impact, claims)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, filtered, plan)
}

// TriggerDatasetLineageBuilds ports `trigger_dataset_lineage_builds`.
func (h *Handlers) TriggerDatasetLineageBuilds(w http.ResponseWriter, r *http.Request) {
	claims, _ := authmw.FromContext(r.Context())
	datasetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var body models.LineageBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := lineage.EnsureDatasetSnapshot(r.Context(), h.State, datasetID); err != nil {
		slog.Warn("dataset snapshot refresh failed", slog.String("dataset_id", datasetID.String()), slog.String("error", err.Error()))
	}

	impactPlan := lineage.HotPathQueryPlan(queryrouter.KindDatasetImpact)
	impact, err := lineage.GetLineageImpactAnalysis(r.Context(), h.State, datasetID, impactPlan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if impact == nil {
		writeStatus(w, http.StatusNotFound, queryrouter.QueryPlan{})
		return
	}
	if _, err := lineage.FilterImpactForClaims(*impact, claims); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	result, err := lineage.TriggerLineageBuilds(r.Context(), h.State, datasetID, claims.Sub, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result, queryrouter.QueryPlan{})
}

// SyncWorkflowLineage ports `sync_workflow_lineage`.
func (h *Handlers) SyncWorkflowLineage(w http.ResponseWriter, r *http.Request) {
	workflowID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var body models.WorkflowLineageSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := lineage.SyncWorkflowLineage(r.Context(), h.State, workflowID, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteWorkflowLineage ports `delete_workflow_lineage`.
func (h *Handlers) DeleteWorkflowLineage(w http.ResponseWriter, r *http.Request) {
	workflowID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	if err := lineage.DeleteWorkflowLineage(r.Context(), h.State, workflowID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
