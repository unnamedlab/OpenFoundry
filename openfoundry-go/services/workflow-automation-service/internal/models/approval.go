package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

// InternalApprovalContinuationRequest mirrors
// models::approval::InternalApprovalContinuationRequest. The payload of
// `POST /api/v1/workflows/approvals/{id}/continue`.
type InternalApprovalContinuationRequest struct {
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	WorkflowRunID uuid.UUID       `json:"workflow_run_id"`
	StepID        string          `json:"step_id"`
	Decision      string          `json:"decision"`
	Context       json.RawMessage `json:"context,omitempty"`
}
