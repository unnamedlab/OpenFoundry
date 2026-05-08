package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/serving"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// DeploymentsHandlers ports libs/ml-kernel/src/handlers/deployments.rs:
//   - GET   list_deployments
//   - POST  create_deployment        (normalises traffic_split for
//     ab_test vs single strategies;
//     marks model.active_deployment_id)
//   - PATCH update_deployment        (re-normalises split; flips the
//     model's active_deployment_id
//     based on the new status)
//   - POST  generate_drift_report    (calls domain.GenerateDriftReport
//     and optionally enqueues a
//     drift-recovery training job)
type DeploymentsHandlers struct {
	Pool    *pgxpool.Pool
	Store   DeploymentStore
	Runtime serving.DeploymentRuntime
}

const deploymentColumns = `id, model_id, name, status, strategy_type,
                           endpoint_path, traffic_split,
                           monitoring_window, baseline_dataset_id,
                           drift_report, created_at, updated_at`

func scanDeployment(s predictionsScanner) (models.ModelDeployment, error) {
	var d models.ModelDeployment
	var splitRaw, driftRaw []byte
	var baselineDatasetID *uuid.UUID
	if err := s.Scan(
		&d.ID, &d.ModelID, &d.Name, &d.Status, &d.StrategyType,
		&d.EndpointPath, &splitRaw, &d.MonitoringWindow,
		&baselineDatasetID, &driftRaw, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return d, err
	}
	d.BaselineDatasetID = baselineDatasetID
	if len(splitRaw) > 0 {
		_ = json.Unmarshal(splitRaw, &d.TrafficSplit)
	}
	if d.TrafficSplit == nil {
		d.TrafficSplit = []models.TrafficSplitEntry{}
	}
	if len(driftRaw) > 0 && string(driftRaw) != "null" {
		var report models.DriftReport
		if err := json.Unmarshal(driftRaw, &report); err == nil {
			d.DriftReport = &report
		}
	}
	return d, nil
}

func (h *DeploymentsHandlers) loadDeployment(ctx context.Context, id uuid.UUID) (*models.ModelDeployment, error) {
	return h.deploymentStore().GetDeployment(ctx, id)
}

func (h *DeploymentsHandlers) deploymentRuntime() serving.DeploymentRuntime {
	if h.Runtime != nil {
		return h.Runtime
	}
	return serving.UnavailableDeploymentRuntime{Reason: "deployment runtime not configured"}
}

// normaliseTrafficSplit mirrors the Rust normalize_traffic_split:
// labels default to "variant-N", non-ab_test strategies collapse to
// the first split with allocation=100, and ab_test allocations are
// proportionally scaled to sum to exactly 100 (with the last entry
// soaking up any remainder so rounding doesn't drift).
func normaliseTrafficSplit(strategyType string, splits []models.TrafficSplitEntry) ([]models.TrafficSplitEntry, error) {
	if len(splits) == 0 {
		return nil, errors.New("at least one traffic split entry is required")
	}
	out := make([]models.TrafficSplitEntry, len(splits))
	for i, s := range splits {
		if strings.TrimSpace(s.Label) == "" {
			s.Label = fmt.Sprintf("variant-%d", i+1)
		}
		out[i] = s
	}
	if strategyType != "ab_test" {
		first := out[0]
		first.Allocation = 100
		return []models.TrafficSplitEntry{first}, nil
	}
	var total uint32
	for _, s := range out {
		total += uint32(s.Allocation)
	}
	if total == 0 {
		return nil, errors.New("traffic allocation must be greater than zero")
	}

	normalised := make([]models.TrafficSplitEntry, 0, len(out))
	remaining := uint32(100)
	lastIdx := len(out) - 1
	for i, s := range out {
		var allocation uint8
		if i == lastIdx {
			if remaining > 255 {
				remaining = 255
			}
			allocation = uint8(remaining)
		} else {
			scaled := uint32(math.Round(float64(s.Allocation) / float64(total) * 100.0))
			if scaled > remaining {
				scaled = remaining
			}
			remaining -= scaled
			allocation = uint8(scaled)
		}
		s.Allocation = allocation
		normalised = append(normalised, s)
	}
	return normalised, nil
}

// ListDeployments handles `GET /api/v1/deployments`.
func (h *DeploymentsHandlers) ListDeployments(w http.ResponseWriter, r *http.Request) {
	deployments, err := h.deploymentStore().ListDeployments(r.Context())
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListDeploymentsResponse{Data: deployments})
}

// GetDeployment handles `GET /api/v1/deployments/{id}`.
func (h *DeploymentsHandlers) GetDeployment(w http.ResponseWriter, r *http.Request, deploymentID uuid.UUID) {
	deployment, err := h.deploymentStore().GetDeployment(r.Context(), deploymentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if deployment == nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	writeJSON(w, http.StatusOK, deployment)
}

// CreateDeployment handles `POST /api/v1/deployments`.
func (h *DeploymentsHandlers) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	var body models.CreateDeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.EndpointPath) == "" {
		writeError(w, http.StatusBadRequest, "deployment name and endpoint path are required")
		return
	}
	store := h.deploymentStore()
	if err := validateDeploymentRefs(r.Context(), store, body.ModelID, body.TrafficSplit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	strategyType := body.StrategyType
	if strategyType == "" {
		strategyType = models.DefaultDeploymentStrategyType
	}
	monitoringWindow := body.MonitoringWindow
	if monitoringWindow == "" {
		monitoringWindow = models.DefaultDeploymentMonitoringWindow
	}

	splits, err := normaliseTrafficSplit(strategyType, body.TrafficSplit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	deployment := models.ModelDeployment{
		ID:                uuid.New(),
		ModelID:           body.ModelID,
		Name:              strings.TrimSpace(body.Name),
		Status:            "active",
		StrategyType:      strategyType,
		EndpointPath:      strings.TrimSpace(body.EndpointPath),
		TrafficSplit:      splits,
		MonitoringWindow:  monitoringWindow,
		BaselineDatasetID: body.BaselineDatasetID,
	}
	if err := h.deploymentRuntime().Deploy(r.Context(), deployment); err != nil {
		writeError(w, runtimeStatus(err), err.Error())
		return
	}
	created, err := store.CreateDeployment(r.Context(), deployment)
	if err != nil {
		dbError(w, err)
		return
	}
	if err := store.SetActiveDeployment(r.Context(), created.ModelID, &created.ID); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// UpdateDeployment handles `PATCH /api/v1/deployments/{id}`.
func (h *DeploymentsHandlers) UpdateDeployment(w http.ResponseWriter, r *http.Request, deploymentID uuid.UUID) {
	store := h.deploymentStore()
	current, err := store.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}

	var body models.UpdateDeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	strategyType := current.StrategyType
	if body.StrategyType != nil {
		strategyType = *body.StrategyType
	}
	splits := current.TrafficSplit
	if body.TrafficSplit != nil {
		if err := validateDeploymentRefs(r.Context(), store, current.ModelID, *body.TrafficSplit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		splits = *body.TrafficSplit
	}
	normalised, err := normaliseTrafficSplit(strategyType, splits)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := *current
	updated.Status = derefString(body.Status, current.Status)
	updated.Name = derefString(body.Name, current.Name)
	updated.StrategyType = strategyType
	updated.EndpointPath = derefString(body.EndpointPath, current.EndpointPath)
	updated.TrafficSplit = normalised
	updated.MonitoringWindow = derefString(body.MonitoringWindow, current.MonitoringWindow)
	if body.BaselineDatasetID != nil {
		updated.BaselineDatasetID = body.BaselineDatasetID
	}
	if updated.Status != current.Status {
		if err := h.deploymentRuntime().Transition(r.Context(), updated, updated.Status); err != nil {
			writeError(w, runtimeStatus(err), err.Error())
			return
		}
	}

	deployment, err := store.UpdateDeployment(r.Context(), updated)
	if err != nil {
		dbError(w, err)
		return
	}
	if deployment.Status == "active" {
		if err := store.SetActiveDeployment(r.Context(), deployment.ModelID, &deployment.ID); err != nil {
			dbError(w, err)
			return
		}
	} else {
		if err := store.SetActiveDeployment(r.Context(), deployment.ModelID, nil); err != nil {
			dbError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, deployment)
}

func validateDeploymentRefs(ctx context.Context, store DeploymentStore, modelID uuid.UUID, splits []models.TrafficSplitEntry) error {
	if modelID == uuid.Nil {
		return errors.New("model_id is required")
	}
	ok, err := store.ModelExists(ctx, modelID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("model not found")
	}
	for _, split := range splits {
		if split.ModelVersionID == uuid.Nil {
			return errors.New("traffic split model_version_id is required")
		}
		ok, err := store.ModelVersionBelongs(ctx, modelID, split.ModelVersionID)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("model version not found for model")
		}
	}
	return nil
}

func runtimeStatus(err error) int {
	if errors.Is(err, serving.ErrRuntimeUnavailable) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

// GenerateDriftReport handles `POST /api/v1/deployments/{id}/drift`.
func (h *DeploymentsHandlers) GenerateDriftReport(w http.ResponseWriter, r *http.Request, deploymentID uuid.UUID) {
	current, err := h.loadDeployment(r.Context(), deploymentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}

	var body models.GenerateDriftReportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	report := domain.GenerateDriftReport(body, len(current.TrafficSplit))

	if report.RecommendRetraining && body.AutoRetrain {
		jobID := uuid.New()
		now := time.Now().UTC()
		datasetIDs, _ := json.Marshal([]string{})
		trainingConfig, _ := json.Marshal(map[string]any{
			"trigger":       "drift-monitor",
			"deployment_id": deploymentID,
			"endpoint_path": current.EndpointPath,
		})
		hyperSearch, _ := json.Marshal(map[string]any{"mode": "drift-triggered"})
		trials, _ := json.Marshal([]any{})

		if _, err := h.Pool.Exec(r.Context(),
			`INSERT INTO ml_training_jobs
                  (id, experiment_id, model_id, name, status,
                   dataset_ids, training_config, hyperparameter_search,
                   objective_metric_name, trials, best_model_version_id,
                   submitted_at, started_at, completed_at, created_at)
                VALUES ($1, NULL, $2, $3, 'queued', $4, $5, $6, $7, $8, NULL, $9, NULL, NULL, $9)`,
			jobID, current.ModelID,
			fmt.Sprintf("Auto retrain for %s", current.Name),
			datasetIDs, trainingConfig, hyperSearch, "drift_recovery",
			trials, now); err != nil {
			dbError(w, err)
			return
		}
		report.AutoRetrainingJobID = &jobID
	}

	reportJSON, _ := json.Marshal(report)
	row := h.Pool.QueryRow(r.Context(),
		`UPDATE ml_deployments SET drift_report = $2, updated_at = NOW()
          WHERE id = $1
          RETURNING `+deploymentColumns,
		deploymentID, reportJSON)
	d, err := scanDeployment(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
