package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/interop"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// runs.go owns the run-tier endpoints exposed via ExperimentsHandlers
// (the Rust source keeps them in handlers/experiments.rs alongside
// experiments). Mirrors fn list_runs / create_run / update_run /
// compare_runs verbatim, including the interop.MergeRunParams /
// MergeRunArtifacts / MergeMetrics + NormalizeTrackingSource folding
// that the new domain/interop port now supplies.

const runColumns = `id, experiment_id, name, status, params, metrics,
                    artifacts, notes, source_dataset_ids,
                    model_version_id, started_at, finished_at,
                    created_at, updated_at`

func scanRun(s predictionsScanner) (models.ExperimentRun, error) {
	var r models.ExperimentRun
	var paramsRaw, metricsRaw, artifactsRaw, sourceDatasetIDsRaw []byte
	var modelVersionID *uuid.UUID
	var startedAt, finishedAt *time.Time
	if err := s.Scan(
		&r.ID, &r.ExperimentID, &r.Name, &r.Status,
		&paramsRaw, &metricsRaw, &artifactsRaw, &r.Notes,
		&sourceDatasetIDsRaw, &modelVersionID,
		&startedAt, &finishedAt, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return r, err
	}
	r.ModelVersionID = modelVersionID
	r.StartedAt = startedAt
	r.FinishedAt = finishedAt
	if len(paramsRaw) > 0 {
		r.Params = paramsRaw
	} else {
		r.Params = json.RawMessage("{}")
	}
	if len(metricsRaw) > 0 {
		_ = json.Unmarshal(metricsRaw, &r.Metrics)
	}
	if r.Metrics == nil {
		r.Metrics = []models.MetricValue{}
	}
	if len(artifactsRaw) > 0 {
		_ = json.Unmarshal(artifactsRaw, &r.Artifacts)
	}
	if r.Artifacts == nil {
		r.Artifacts = []models.ArtifactReference{}
	}
	if len(sourceDatasetIDsRaw) > 0 {
		_ = json.Unmarshal(sourceDatasetIDsRaw, &r.SourceDatasetIDs)
	}
	if r.SourceDatasetIDs == nil {
		r.SourceDatasetIDs = []uuid.UUID{}
	}
	r.ExternalTracking = interop.TrackingSourceFromParams(r.Params)
	return r, nil
}

func (h *ExperimentsHandlers) loadRun(ctx context.Context, runID uuid.UUID) (*models.ExperimentRun, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT `+runColumns+` FROM ml_runs WHERE id = $1`, runID)
	r, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (h *ExperimentsHandlers) experimentExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	if err := h.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM ml_experiments WHERE id = $1)`, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ListRuns handles `GET /api/v1/experiments/{id}/runs`.
func (h *ExperimentsHandlers) ListRuns(w http.ResponseWriter, r *http.Request, experimentID uuid.UUID) {
	exists, err := h.experimentExists(r.Context(), experimentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "experiment not found")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+runColumns+` FROM ml_runs
          WHERE experiment_id = $1
          ORDER BY created_at DESC`, experimentID)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := make([]models.ExperimentRun, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		out = append(out, run)
	}
	writeJSON(w, http.StatusOK, models.ListRunsResponse{Data: out})
}

// CreateRun handles `POST /api/v1/experiments/{id}/runs`. Mirrors fn
// create_run: validates name, defaults status="completed",
// auto-fills started_at/finished_at when missing, folds external
// tracking into params + metrics + artifacts via interop, refreshes
// the experiment rollup so {run_count, best_metric} stay current.
func (h *ExperimentsHandlers) CreateRun(w http.ResponseWriter, r *http.Request, experimentID uuid.UUID) {
	var body models.CreateExperimentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "run name is required")
		return
	}

	exists, err := h.experimentExists(r.Context(), experimentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "experiment not found")
		return
	}

	now := time.Now().UTC()
	status := derefString(body.Status, "completed")
	startedAt := body.StartedAt
	if startedAt == nil {
		startedAt = &now
	}
	finishedAt := body.FinishedAt
	if finishedAt == nil && status == "completed" {
		finishedAt = &now
	}

	var externalTracking *models.ExternalTrackingSource
	if body.ExternalTracking != nil && body.ExternalTracking.HasSignal() {
		n := interop.NormalizeTrackingSource(*body.ExternalTracking)
		externalTracking = &n
	}

	primaryMetrics := body.Metrics
	if primaryMetrics == nil {
		primaryMetrics = []models.MetricValue{}
	}
	var externalMetrics []models.MetricValue
	if externalTracking != nil {
		externalMetrics = externalTracking.Metrics
	}
	metrics := interop.MergeMetrics(primaryMetrics, externalMetrics)

	primaryArtifacts := body.Artifacts
	if primaryArtifacts == nil {
		primaryArtifacts = []models.ArtifactReference{}
	}
	artifacts := interop.MergeRunArtifacts(primaryArtifacts, externalTracking)

	paramsRaw := body.Params
	if len(paramsRaw) == 0 {
		paramsRaw = json.RawMessage("{}")
	}
	params := interop.MergeRunParams(paramsRaw, externalTracking)

	notes := derefString(body.Notes, "")
	metricsJSON, _ := json.Marshal(metrics)
	artifactsJSON, _ := json.Marshal(artifacts)
	sourceDatasetIDs := body.SourceDatasetIDs
	if sourceDatasetIDs == nil {
		sourceDatasetIDs = []uuid.UUID{}
	}
	sourceDatasetIDsJSON, _ := json.Marshal(sourceDatasetIDs)

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ml_runs
              (id, experiment_id, name, status, params, metrics,
               artifacts, notes, source_dataset_ids,
               started_at, finished_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
            RETURNING `+runColumns,
		uuid.New(), experimentID, strings.TrimSpace(body.Name), status,
		params, metricsJSON, artifactsJSON, notes, sourceDatasetIDsJSON,
		startedAt, finishedAt)
	run, err := scanRun(row)
	if err != nil {
		dbError(w, err)
		return
	}
	if err := h.refreshExperimentRollup(r.Context(), experimentID); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// UpdateRun handles `PATCH /api/v1/runs/{id}`. Mirrors fn update_run:
// every field falls back to the current row when not provided;
// external tracking re-folds into params / metrics / artifacts.
func (h *ExperimentsHandlers) UpdateRun(w http.ResponseWriter, r *http.Request, runID uuid.UUID) {
	current, err := h.loadRun(r.Context(), runID)
	if err != nil {
		dbError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	var body models.UpdateExperimentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	status := derefString(body.Status, current.Status)

	var externalTracking *models.ExternalTrackingSource
	if body.ExternalTracking != nil && body.ExternalTracking.HasSignal() {
		n := interop.NormalizeTrackingSource(*body.ExternalTracking)
		externalTracking = &n
	}

	paramsRaw := current.Params
	if body.Params != nil && len(*body.Params) > 0 {
		paramsRaw = *body.Params
	}
	params := interop.MergeRunParams(paramsRaw, externalTracking)

	primaryMetrics := current.Metrics
	if body.Metrics != nil {
		primaryMetrics = *body.Metrics
	}
	var externalMetrics []models.MetricValue
	if externalTracking != nil {
		externalMetrics = externalTracking.Metrics
	}
	metricsJSON, _ := json.Marshal(interop.MergeMetrics(primaryMetrics, externalMetrics))

	primaryArtifacts := current.Artifacts
	if body.Artifacts != nil {
		primaryArtifacts = *body.Artifacts
	}
	artifactsJSON, _ := json.Marshal(interop.MergeRunArtifacts(primaryArtifacts, externalTracking))

	notes := derefString(body.Notes, current.Notes)
	modelVersionID := current.ModelVersionID
	if body.ModelVersionID != nil {
		modelVersionID = body.ModelVersionID
	}
	finishedAt := current.FinishedAt
	if body.FinishedAt != nil {
		finishedAt = body.FinishedAt
	}

	row := h.Pool.QueryRow(r.Context(),
		`UPDATE ml_runs SET
            status = $2, params = $3, metrics = $4, artifacts = $5,
            notes = $6, model_version_id = $7, finished_at = $8,
            updated_at = NOW()
          WHERE id = $1
          RETURNING `+runColumns,
		runID, status, params, metricsJSON, artifactsJSON,
		notes, modelVersionID, finishedAt)
	run, err := scanRun(row)
	if err != nil {
		dbError(w, err)
		return
	}
	if err := h.refreshExperimentRollup(r.Context(), run.ExperimentID); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// CompareRuns handles `POST /api/v1/runs/compare`. Mirrors fn
// compare_runs: loads each run, errors with "run <uuid> not found"
// when any one is missing, returns the union of metric names sorted
// alphabetically (Rust BTreeSet → Go sort.Strings).
func (h *ExperimentsHandlers) CompareRuns(w http.ResponseWriter, r *http.Request) {
	var body models.CompareRunsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.RunIDs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one run is required")
		return
	}

	runs := make([]models.ExperimentRun, 0, len(body.RunIDs))
	for _, runID := range body.RunIDs {
		run, err := h.loadRun(r.Context(), runID)
		if err != nil {
			dbError(w, err)
			return
		}
		if run == nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("run %s not found", runID))
			return
		}
		runs = append(runs, *run)
	}

	metricSet := map[string]struct{}{}
	for _, run := range runs {
		for _, m := range run.Metrics {
			metricSet[m.Name] = struct{}{}
		}
	}
	metricNames := make([]string, 0, len(metricSet))
	for name := range metricSet {
		metricNames = append(metricNames, name)
	}
	sort.Strings(metricNames)

	writeJSON(w, http.StatusOK, models.CompareRunsResponse{
		Data:        runs,
		MetricNames: metricNames,
	})
}
