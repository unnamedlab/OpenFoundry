package models

import (
	"time"

	"github.com/google/uuid"
)

// RegisteredModel is the catalog row for one ML model.
type RegisteredModel struct {
	ID                   uuid.UUID  `json:"id"`
	Name                 string     `json:"name"`
	Description          string     `json:"description"`
	ProblemType          string     `json:"problem_type"`
	Status               string     `json:"status"`
	Tags                 []string   `json:"tags"`
	OwnerID              *uuid.UUID `json:"owner_id"`
	CurrentStage         string     `json:"current_stage"`
	LatestVersionNumber  *int32     `json:"latest_version_number"`
	ActiveDeploymentID   *uuid.UUID `json:"active_deployment_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ListModelsResponse struct {
	Data []RegisteredModel `json:"data"`
}

// CreateModelRequest defaults: problem_type="classification".
type CreateModelRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ProblemType string   `json:"problem_type,omitempty"`
	Status      *string  `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type UpdateModelRequest struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	ProblemType *string   `json:"problem_type,omitempty"`
	Status      *string   `json:"status,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

const DefaultProblemType = "classification"
