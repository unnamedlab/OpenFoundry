package models

import (
	"time"

	"github.com/google/uuid"
)

// TrafficSplitEntry describes one bucket of a deployment's traffic split.
type TrafficSplitEntry struct {
	ModelVersionID uuid.UUID `json:"model_version_id"`
	Label          string    `json:"label"`
	Allocation     uint8     `json:"allocation"`
}

// DriftMetric is a single drift score with threshold + status.
type DriftMetric struct {
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
	Threshold float64 `json:"threshold"`
	Status    string  `json:"status"`
}

// DriftReport is the periodic drift snapshot.
type DriftReport struct {
	GeneratedAt          time.Time     `json:"generated_at"`
	DatasetMetrics       []DriftMetric `json:"dataset_metrics"`
	ConceptMetrics       []DriftMetric `json:"concept_metrics"`
	RecommendRetraining  bool          `json:"recommend_retraining"`
	AutoRetrainingJobID  *uuid.UUID    `json:"auto_retraining_job_id"`
	Notes                string        `json:"notes"`
}

// ModelDeployment is the deployment row.
type ModelDeployment struct {
	ID                uuid.UUID            `json:"id"`
	ModelID           uuid.UUID            `json:"model_id"`
	Name              string               `json:"name"`
	Status            string               `json:"status"`
	StrategyType      string               `json:"strategy_type"`
	EndpointPath      string               `json:"endpoint_path"`
	TrafficSplit      []TrafficSplitEntry  `json:"traffic_split"`
	MonitoringWindow  string               `json:"monitoring_window"`
	BaselineDatasetID *uuid.UUID           `json:"baseline_dataset_id"`
	DriftReport       *DriftReport         `json:"drift_report"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type ListDeploymentsResponse struct {
	Data []ModelDeployment `json:"data"`
}

// CreateDeploymentRequest defaults: strategy_type="single",
// monitoring_window="24h".
type CreateDeploymentRequest struct {
	ModelID           uuid.UUID           `json:"model_id"`
	Name              string              `json:"name"`
	StrategyType      string              `json:"strategy_type,omitempty"`
	EndpointPath      string              `json:"endpoint_path"`
	TrafficSplit      []TrafficSplitEntry `json:"traffic_split,omitempty"`
	MonitoringWindow  string              `json:"monitoring_window,omitempty"`
	BaselineDatasetID *uuid.UUID          `json:"baseline_dataset_id,omitempty"`
}

type UpdateDeploymentRequest struct {
	Name              *string              `json:"name,omitempty"`
	Status            *string              `json:"status,omitempty"`
	StrategyType      *string              `json:"strategy_type,omitempty"`
	EndpointPath      *string              `json:"endpoint_path,omitempty"`
	TrafficSplit      *[]TrafficSplitEntry `json:"traffic_split,omitempty"`
	MonitoringWindow  *string              `json:"monitoring_window,omitempty"`
	BaselineDatasetID *uuid.UUID           `json:"baseline_dataset_id,omitempty"`
}

type GenerateDriftReportRequest struct {
	BaselineRows *int64 `json:"baseline_rows,omitempty"`
	ObservedRows *int64 `json:"observed_rows,omitempty"`
	AutoRetrain  bool   `json:"auto_retrain,omitempty"`
}

const (
	DefaultDeploymentStrategyType    = "single"
	DefaultDeploymentMonitoringWindow = "24h"
)
