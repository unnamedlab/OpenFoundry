package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// FeatureContribution is one feature's contribution to a prediction.
type FeatureContribution struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// PredictionOutput is one row of a prediction batch.
type PredictionOutput struct {
	RecordID        string                `json:"record_id"`
	Variant         string                `json:"variant"`
	ModelVersionID  uuid.UUID             `json:"model_version_id"`
	PredictedLabel  string                `json:"predicted_label"`
	Score           float64               `json:"score"`
	Confidence      float64               `json:"confidence"`
	Contributions   []FeatureContribution `json:"contributions"`
}

type RealtimePredictionRequest struct {
	Inputs  []json.RawMessage `json:"inputs,omitempty"`
	Explain bool              `json:"explain,omitempty"`
}

type RealtimePredictionResponse struct {
	DeploymentID uuid.UUID          `json:"deployment_id"`
	Outputs      []PredictionOutput `json:"outputs"`
	PredictedAt  time.Time          `json:"predicted_at"`
}

type CreateBatchPredictionRequest struct {
	DeploymentID      uuid.UUID         `json:"deployment_id"`
	Records           []json.RawMessage `json:"records,omitempty"`
	OutputDestination *string           `json:"output_destination"`
}

type BatchPredictionJob struct {
	ID                uuid.UUID          `json:"id"`
	DeploymentID      uuid.UUID          `json:"deployment_id"`
	Status            string             `json:"status"`
	RecordCount       int64              `json:"record_count"`
	OutputDestination *string            `json:"output_destination"`
	Outputs           []PredictionOutput `json:"outputs"`
	CreatedAt         time.Time          `json:"created_at"`
	CompletedAt       *time.Time         `json:"completed_at"`
}

type ListBatchPredictionsResponse struct {
	Data []BatchPredictionJob `json:"data"`
}
