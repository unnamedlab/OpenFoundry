package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Pipeline mirrors the subset of the canonical `Pipeline` struct that
// lineage-service actually consumes. The Rust source uses
// `#[path = "../../../pipeline-authoring-service/src/models/pipeline.rs"]`
// to alias the full struct; the Go side keeps a local copy because
// internal packages can't cross service boundaries.
//
// Only the fields read by lineage code paths are included. Adding
// more later is a non-breaking append.
type Pipeline struct {
	ID             uuid.UUID       `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	OwnerID        uuid.UUID       `json:"owner_id"`
	DAG            json.RawMessage `json:"dag"`
	Status         string          `json:"status"`
	ScheduleConfig json.RawMessage `json:"schedule_config"`
	RetryPolicy    json.RawMessage `json:"retry_policy"`
	NextRunAt      *time.Time      `json:"next_run_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// PipelineRun mirrors the runtime row returned by `executor::start_pipeline_run`.
//
// Lineage's `trigger_lineage_builds` path only reads `id` + `status`
// from the pipeline run, so we keep this struct intentionally narrow.
type PipelineRun struct {
	ID         uuid.UUID `json:"id"`
	PipelineID uuid.UUID `json:"pipeline_id"`
	Status     string    `json:"status"`
}
