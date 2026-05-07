package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ModelVersion is one immutable snapshot of a registered model.
type ModelVersion struct {
	ID                uuid.UUID                 `json:"id"`
	ModelID           uuid.UUID                 `json:"model_id"`
	VersionNumber     int32                     `json:"version_number"`
	VersionLabel      string                    `json:"version_label"`
	Stage             string                    `json:"stage"`
	SourceRunID       *uuid.UUID                `json:"source_run_id"`
	TrainingJobID     *uuid.UUID                `json:"training_job_id"`
	Hyperparameters   json.RawMessage           `json:"hyperparameters"`
	Metrics           []MetricValue             `json:"metrics"`
	ArtifactURI       *string                   `json:"artifact_uri"`
	Schema            json.RawMessage           `json:"schema"`
	ModelAdapter      *ModelAdapterDescriptor   `json:"model_adapter,omitempty"`
	RegistrySource    *RegistrySourceDescriptor `json:"registry_source,omitempty"`
	ExternalTracking  *ExternalTrackingSource   `json:"external_tracking,omitempty"`
	CreatedAt         time.Time                 `json:"created_at"`
	PromotedAt        *time.Time                `json:"promoted_at"`
}

type ListModelVersionsResponse struct {
	Data []ModelVersion `json:"data"`
}

type CreateModelVersionRequest struct {
	VersionLabel     *string                   `json:"version_label,omitempty"`
	Stage            *string                   `json:"stage,omitempty"`
	SourceRunID      *uuid.UUID                `json:"source_run_id,omitempty"`
	TrainingJobID    *uuid.UUID                `json:"training_job_id,omitempty"`
	Hyperparameters  *json.RawMessage          `json:"hyperparameters,omitempty"`
	Metrics          *[]MetricValue            `json:"metrics,omitempty"`
	ArtifactURI      *string                   `json:"artifact_uri,omitempty"`
	Schema           *json.RawMessage          `json:"schema,omitempty"`
	ModelAdapter     *ModelAdapterDescriptor   `json:"model_adapter,omitempty"`
	RegistrySource   *RegistrySourceDescriptor `json:"registry_source,omitempty"`
	ExternalTracking *ExternalTrackingSource   `json:"external_tracking,omitempty"`
}

type TransitionModelVersionRequest struct {
	Stage string `json:"stage"`
}
