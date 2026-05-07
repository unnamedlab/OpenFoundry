package approvals

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WorkflowApproval mirrors `approvals::models::approval::WorkflowApproval`.
type WorkflowApproval struct {
	ID            uuid.UUID       `json:"id"`
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	WorkflowRunID uuid.UUID       `json:"workflow_run_id"`
	StepID        string          `json:"step_id"`
	Title         string          `json:"title"`
	Instructions  string          `json:"instructions"`
	AssignedTo    *uuid.UUID      `json:"assigned_to"`
	Status        string          `json:"status"`
	Decision      *string         `json:"decision"`
	Payload       json.RawMessage `json:"payload"`
	RequestedAt   time.Time       `json:"requested_at"`
	DecidedAt     *time.Time      `json:"decided_at"`
	DecidedBy     *uuid.UUID      `json:"decided_by"`
}

// CreateApprovalRequest mirrors the Rust struct of the same name.
type CreateApprovalRequest struct {
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	WorkflowRunID uuid.UUID       `json:"workflow_run_id"`
	StepID        string          `json:"step_id"`
	Title         string          `json:"title"`
	Instructions  string          `json:"instructions"`
	AssignedTo    *uuid.UUID      `json:"assigned_to"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// CreateApprovalResponse mirrors the Rust struct of the same name.
type CreateApprovalResponse struct {
	Approval WorkflowApproval `json:"approval"`
	Created  bool             `json:"created"`
}

// ListApprovalsQuery mirrors the Rust struct of the same name.
type ListApprovalsQuery struct {
	Page       *int64
	PerPage    *int64
	Status     *string
	AssignedTo *uuid.UUID
	WorkflowID *uuid.UUID
}

// ApprovalDecisionRequest mirrors the Rust struct of the same name.
type ApprovalDecisionRequest struct {
	Decision string          `json:"decision"`
	Comment  *string         `json:"comment,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}
