// Package models holds the wire-format types for the local
// adapter + lifecycle surfaces of model-catalog-service.
//
// Models for the kernel-bound `models` + `experiments` surfaces
// land alongside libs/ml-kernel-go.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// --- Adapter ---------------------------------------------------------

// ModelAdapter mirrors model_adapters.
type ModelAdapter struct {
	ID           uuid.UUID  `json:"id"`
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	AdapterKind  string     `json:"adapter_kind"`
	ArtifactURI  string     `json:"artifact_uri"`
	SidecarImage *string    `json:"sidecar_image"`
	Framework    *string    `json:"framework"`
	ModelID      *uuid.UUID `json:"model_id"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type RegisterAdapterRequest struct {
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	AdapterKind  string     `json:"adapter_kind"`
	ArtifactURI  string     `json:"artifact_uri"`
	SidecarImage *string    `json:"sidecar_image,omitempty"`
	Framework    *string    `json:"framework,omitempty"`
	ModelID      *uuid.UUID `json:"model_id,omitempty"`
}

// InferenceContract mirrors inference_contracts.
type InferenceContract struct {
	ID           uuid.UUID       `json:"id"`
	AdapterID    uuid.UUID       `json:"adapter_id"`
	Version      string          `json:"version"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	CreatedAt    time.Time       `json:"created_at"`
}

type PublishContractRequest struct {
	Version      string          `json:"version"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

// --- Lifecycle -------------------------------------------------------

// ModelSubmission mirrors model_submissions.
type ModelSubmission struct {
	ID           uuid.UUID  `json:"id"`
	ModelID      uuid.UUID  `json:"model_id"`
	Version      string     `json:"version"`
	Stage        string     `json:"stage"`
	Status       string     `json:"status"`
	ObjectiveID  *uuid.UUID `json:"objective_id"`
	ReleaseNotes *string    `json:"release_notes"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type CreateSubmissionRequest struct {
	ModelID      uuid.UUID  `json:"model_id"`
	Version      string     `json:"version"`
	ObjectiveID  *uuid.UUID `json:"objective_id,omitempty"`
	ReleaseNotes *string    `json:"release_notes,omitempty"`
}

type TransitionRequest struct {
	Stage  string  `json:"stage"`
	Status string  `json:"status"`
	Note   *string `json:"note,omitempty"`
}

// ModelingObjective mirrors modeling_objectives.
type ModelingObjective struct {
	ID              uuid.UUID       `json:"id"`
	Slug            string          `json:"slug"`
	Name            string          `json:"name"`
	Description     *string         `json:"description"`
	SuccessCriteria json.RawMessage `json:"success_criteria"`
	CreatedAt       time.Time       `json:"created_at"`
}

type CreateObjectiveRequest struct {
	Slug            string          `json:"slug"`
	Name            string          `json:"name"`
	Description     *string         `json:"description,omitempty"`
	SuccessCriteria json.RawMessage `json:"success_criteria"`
}
