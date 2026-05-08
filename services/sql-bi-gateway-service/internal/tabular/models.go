// Package tabular ports services/sql-bi-gateway-service/src/tabular/.
//
// Persistent models and HTTP handlers for the tabular-analysis bounded
// context, ported verbatim from the retired tabular-analysis-service
// (S8 consolidation, ADR-0030). Schema lives in
// migrations/20260427070500_tabular_analysis_foundation.sql.
package tabular

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AnalysisJob is a single tabular-analysis job row.
type AnalysisJob struct {
	ID           uuid.UUID       `json:"id"`
	DatasetID    uuid.UUID       `json:"dataset_id"`
	AnalysisKind string          `json:"analysis_kind"`
	Status       string          `json:"status"`
	Options      json.RawMessage `json:"options"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// SubmitJobRequest is the body for POST /api/v1/tabular/jobs.
type SubmitJobRequest struct {
	DatasetID    uuid.UUID       `json:"dataset_id"`
	AnalysisKind string          `json:"analysis_kind"`
	Options      json.RawMessage `json:"options,omitempty"`
}

// AnalysisResult is a published result for an analysis job.
type AnalysisResult struct {
	ID         uuid.UUID       `json:"id"`
	JobID      uuid.UUID       `json:"job_id"`
	ResultKind string          `json:"result_kind"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

// PublishResultRequest is the body for POST /api/v1/tabular/jobs/:id/results.
type PublishResultRequest struct {
	ResultKind string          `json:"result_kind"`
	Payload    json.RawMessage `json:"payload"`
}
