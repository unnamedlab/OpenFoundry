// Package models holds the wire-format types for workflow-automation-service.
//
// Snake_case JSON tags + field order mirror the Rust serde encoding
// 1:1 so producers + UI clients can round-trip payloads against
// either runtime.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WorkflowBranchCondition mirrors models::workflow::WorkflowBranchCondition.
type WorkflowBranchCondition struct {
	Field    string          `json:"field"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value"`
}

// WorkflowBranch mirrors models::workflow::WorkflowBranch.
type WorkflowBranch struct {
	Condition  WorkflowBranchCondition `json:"condition"`
	NextStepID string                  `json:"next_step_id"`
}

// WorkflowStep mirrors models::workflow::WorkflowStep.
type WorkflowStep struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	StepType    string           `json:"step_type"`
	Description string           `json:"description,omitempty"`
	Config      json.RawMessage  `json:"config,omitempty"`
	NextStepID  *string          `json:"next_step_id,omitempty"`
	Branches    []WorkflowBranch `json:"branches,omitempty"`
}

// WorkflowDefinition mirrors models::workflow::WorkflowDefinition. Rust
// uses sqlx FromRow; in Go we hydrate manually in repo helpers.
type WorkflowDefinition struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	OwnerID         uuid.UUID       `json:"owner_id"`
	Status          string          `json:"status"`
	TriggerType     string          `json:"trigger_type"`
	TriggerConfig   json.RawMessage `json:"trigger_config"`
	Steps           json.RawMessage `json:"steps"`
	WebhookSecret   *string         `json:"webhook_secret"`
	NextRunAt       *time.Time      `json:"next_run_at"`
	LastTriggeredAt *time.Time      `json:"last_triggered_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// ParsedSteps mirrors WorkflowDefinition::parsed_steps.
func (w *WorkflowDefinition) ParsedSteps() ([]WorkflowStep, error) {
	if len(w.Steps) == 0 {
		return nil, nil
	}
	var out []WorkflowStep
	if err := json.Unmarshal(w.Steps, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWorkflowRequest mirrors models::workflow::CreateWorkflowRequest.
type CreateWorkflowRequest struct {
	Name          string          `json:"name"`
	Description   *string         `json:"description,omitempty"`
	Status        *string         `json:"status,omitempty"`
	TriggerType   string          `json:"trigger_type"`
	TriggerConfig json.RawMessage `json:"trigger_config,omitempty"`
	Steps         []WorkflowStep  `json:"steps"`
}

// UpdateWorkflowRequest mirrors models::workflow::UpdateWorkflowRequest.
type UpdateWorkflowRequest struct {
	Name          *string          `json:"name,omitempty"`
	Description   *string          `json:"description,omitempty"`
	Status        *string          `json:"status,omitempty"`
	TriggerType   *string          `json:"trigger_type,omitempty"`
	TriggerConfig *json.RawMessage `json:"trigger_config,omitempty"`
	Steps         *[]WorkflowStep  `json:"steps,omitempty"`
}

// ListWorkflowsQuery mirrors models::workflow::ListWorkflowsQuery.
type ListWorkflowsQuery struct {
	Page        *int64
	PerPage     *int64
	Search      *string
	TriggerType *string
	Status      *string
}
