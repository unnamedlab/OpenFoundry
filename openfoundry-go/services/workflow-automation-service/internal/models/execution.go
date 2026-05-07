package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WorkflowRun mirrors models::execution::WorkflowRun.
type WorkflowRun struct {
	ID            uuid.UUID       `json:"id"`
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	TriggerType   string          `json:"trigger_type"`
	Status        string          `json:"status"`
	StartedBy     *uuid.UUID      `json:"started_by"`
	CurrentStepID *string         `json:"current_step_id"`
	Context       json.RawMessage `json:"context"`
	ErrorMessage  *string         `json:"error_message"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

// StartRunRequest mirrors models::execution::StartRunRequest.
type StartRunRequest struct {
	Context json.RawMessage `json:"context,omitempty"`
}

// TriggerEventRequest mirrors models::execution::TriggerEventRequest.
type TriggerEventRequest struct {
	Context json.RawMessage `json:"context,omitempty"`
}

// InternalLineageRunRequest mirrors models::execution::InternalLineageRunRequest.
type InternalLineageRunRequest struct {
	Context json.RawMessage `json:"context,omitempty"`
}

// InternalTriggeredRunRequest aliases the WorkflowTriggerRequested
// payload from libs/event-bus-control. Mirrors the Rust type alias
// `pub type InternalTriggeredRunRequest = WorkflowTriggerRequested`.
type InternalTriggeredRunRequest struct {
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	TriggerType   string          `json:"trigger_type"`
	StartedBy     *uuid.UUID      `json:"started_by,omitempty"`
	Context       json.RawMessage `json:"context,omitempty"`
	CorrelationID uuid.UUID       `json:"correlation_id,omitempty"`
}

// ListRunsQuery mirrors models::execution::ListRunsQuery.
type ListRunsQuery struct {
	Page    *int64
	PerPage *int64
}
