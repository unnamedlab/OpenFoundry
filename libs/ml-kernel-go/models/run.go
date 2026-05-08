package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MetricValue is one named metric.
type MetricValue struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// ArtifactReference points at one stored artifact.
type ArtifactReference struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	URI          string    `json:"uri"`
	ArtifactType string    `json:"artifact_type"`
	SizeBytes    int64     `json:"size_bytes"`
}

// ExperimentRun is one experiment trial.
type ExperimentRun struct {
	ID                uuid.UUID                `json:"id"`
	ExperimentID      uuid.UUID                `json:"experiment_id"`
	Name              string                   `json:"name"`
	Status            string                   `json:"status"`
	Params            json.RawMessage          `json:"params"`
	Metrics           []MetricValue            `json:"metrics"`
	Artifacts         []ArtifactReference      `json:"artifacts"`
	Notes             string                   `json:"notes"`
	SourceDatasetIDs  []uuid.UUID              `json:"source_dataset_ids"`
	ModelVersionID    *uuid.UUID               `json:"model_version_id"`
	ExternalTracking  *ExternalTrackingSource  `json:"external_tracking,omitempty"`
	StartedAt         *time.Time               `json:"started_at"`
	FinishedAt        *time.Time               `json:"finished_at"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

type ListRunsResponse struct {
	Data []ExperimentRun `json:"data"`
}

type CreateExperimentRunRequest struct {
	Name             string                  `json:"name"`
	Status           *string                 `json:"status,omitempty"`
	Params           json.RawMessage         `json:"params,omitempty"`
	Metrics          []MetricValue           `json:"metrics,omitempty"`
	Artifacts        []ArtifactReference     `json:"artifacts,omitempty"`
	Notes            *string                 `json:"notes,omitempty"`
	SourceDatasetIDs []uuid.UUID             `json:"source_dataset_ids,omitempty"`
	ExternalTracking *ExternalTrackingSource `json:"external_tracking,omitempty"`
	StartedAt        *time.Time              `json:"started_at,omitempty"`
	FinishedAt       *time.Time              `json:"finished_at,omitempty"`
}

type UpdateExperimentRunRequest struct {
	Status           *string                 `json:"status,omitempty"`
	Params           *json.RawMessage        `json:"params,omitempty"`
	Metrics          *[]MetricValue          `json:"metrics,omitempty"`
	Artifacts        *[]ArtifactReference    `json:"artifacts,omitempty"`
	Notes            *string                 `json:"notes,omitempty"`
	ModelVersionID   *uuid.UUID              `json:"model_version_id,omitempty"`
	ExternalTracking *ExternalTrackingSource `json:"external_tracking,omitempty"`
	FinishedAt       *time.Time              `json:"finished_at,omitempty"`
}

type CompareRunsRequest struct {
	RunIDs []uuid.UUID `json:"run_ids"`
}

type CompareRunsResponse struct {
	Data        []ExperimentRun `json:"data"`
	MetricNames []string        `json:"metric_names"`
}
