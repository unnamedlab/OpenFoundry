package models

import (
	"time"

	"github.com/google/uuid"
)

// ModelingObjectiveSpec captures what the experiment is trying to do.
// Defaults: status="draft".
type ModelingObjectiveSpec struct {
	Status              string      `json:"status"`
	DeploymentTarget    string      `json:"deployment_target,omitempty"`
	Stakeholders        []string    `json:"stakeholders,omitempty"`
	SuccessCriteria     []string    `json:"success_criteria,omitempty"`
	LinkedDatasetIDs    []uuid.UUID `json:"linked_dataset_ids,omitempty"`
	LinkedModelIDs      []uuid.UUID `json:"linked_model_ids,omitempty"`
	DocumentationURI    string      `json:"documentation_uri,omitempty"`
	CollaborationNotes  []string    `json:"collaboration_notes,omitempty"`
}

// Experiment is the catalog row for an ML experiment.
type Experiment struct {
	ID            uuid.UUID             `json:"id"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Objective     string                `json:"objective"`
	ObjectiveSpec ModelingObjectiveSpec `json:"objective_spec"`
	TaskType      string                `json:"task_type"`
	PrimaryMetric string                `json:"primary_metric"`
	Status        string                `json:"status"`
	Tags          []string              `json:"tags"`
	RunCount      int64                 `json:"run_count"`
	BestMetric    *MetricValue          `json:"best_metric"`
	OwnerID       *uuid.UUID            `json:"owner_id"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type ListExperimentsResponse struct {
	Data []Experiment `json:"data"`
}

// CreateExperimentRequest defaults: task_type="classification",
// primary_metric="accuracy".
type CreateExperimentRequest struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description,omitempty"`
	Objective     string                 `json:"objective,omitempty"`
	TaskType      string                 `json:"task_type,omitempty"`
	PrimaryMetric string                 `json:"primary_metric,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	ObjectiveSpec *ModelingObjectiveSpec `json:"objective_spec,omitempty"`
}

type UpdateExperimentRequest struct {
	Name          *string                `json:"name,omitempty"`
	Description   *string                `json:"description,omitempty"`
	Objective     *string                `json:"objective,omitempty"`
	TaskType      *string                `json:"task_type,omitempty"`
	PrimaryMetric *string                `json:"primary_metric,omitempty"`
	Status        *string                `json:"status,omitempty"`
	Tags          *[]string              `json:"tags,omitempty"`
	ObjectiveSpec *ModelingObjectiveSpec `json:"objective_spec,omitempty"`
}

const (
	DefaultExperimentTaskType      = "classification"
	DefaultExperimentPrimaryMetric = "accuracy"
	DefaultObjectiveStatus         = "draft"
)
