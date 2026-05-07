package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TrainingTrial is one trial inside an HPO sweep.
type TrainingTrial struct {
	ID              string          `json:"id"`
	Status          string          `json:"status"`
	Hyperparameters json.RawMessage `json:"hyperparameters"`
	ObjectiveMetric MetricValue     `json:"objective_metric"`
}

// TrainingJob is the long-running training task envelope.
type TrainingJob struct {
	ID                  uuid.UUID                `json:"id"`
	ExperimentID        *uuid.UUID               `json:"experiment_id"`
	ModelID             *uuid.UUID               `json:"model_id"`
	Name                string                   `json:"name"`
	Status              string                   `json:"status"`
	DatasetIDs          []uuid.UUID              `json:"dataset_ids"`
	TrainingConfig      json.RawMessage          `json:"training_config"`
	HyperparameterSearch json.RawMessage         `json:"hyperparameter_search"`
	ObjectiveMetricName string                   `json:"objective_metric_name"`
	Trials              []TrainingTrial          `json:"trials"`
	BestModelVersionID  *uuid.UUID               `json:"best_model_version_id"`
	ExternalTraining    *ExternalTrackingSource  `json:"external_training,omitempty"`
	SubmittedAt         time.Time                `json:"submitted_at"`
	StartedAt           *time.Time               `json:"started_at"`
	CompletedAt         *time.Time               `json:"completed_at"`
	CreatedAt           time.Time                `json:"created_at"`
}

type ListTrainingJobsResponse struct {
	Data []TrainingJob `json:"data"`
}

type CreateTrainingJobRequest struct {
	ExperimentID              *uuid.UUID              `json:"experiment_id,omitempty"`
	ModelID                   *uuid.UUID              `json:"model_id,omitempty"`
	Name                      string                  `json:"name"`
	DatasetIDs                []uuid.UUID             `json:"dataset_ids,omitempty"`
	TrainingConfig            json.RawMessage         `json:"training_config,omitempty"`
	HyperparameterSearch      *json.RawMessage        `json:"hyperparameter_search,omitempty"`
	ObjectiveMetricName       *string                 `json:"objective_metric_name,omitempty"`
	AutoRegisterModelVersion  bool                    `json:"auto_register_model_version,omitempty"`
	ExternalTraining          *ExternalTrackingSource `json:"external_training,omitempty"`
}
