package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentMemorySnapshot is the rolling memory the planner reads + writes.
type AgentMemorySnapshot struct {
	ShortTermNotes      []string `json:"short_term_notes,omitempty"`
	LongTermReferences  []string `json:"long_term_references,omitempty"`
	LastRunSummary      *string  `json:"last_run_summary"`
}

// AgentPlanStep is one step in the planner's plan-act-observe loop.
type AgentPlanStep struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	ToolName    *string `json:"tool_name"`
	Status      string  `json:"status"`
}

// AgentExecutionTrace captures one observation/output pair.
type AgentExecutionTrace struct {
	StepID      string          `json:"step_id"`
	Title       string          `json:"title"`
	ToolName    *string         `json:"tool_name"`
	Observation string          `json:"observation"`
	Output      json.RawMessage `json:"output"`
}

// AgentDefinition is the catalog row for one agent.
type AgentDefinition struct {
	ID                uuid.UUID            `json:"id"`
	Name              string               `json:"name"`
	Description       string               `json:"description"`
	Status            string               `json:"status"`
	SystemPrompt      string               `json:"system_prompt"`
	Objective         string               `json:"objective"`
	ToolIDs           []uuid.UUID          `json:"tool_ids"`
	PlanningStrategy  string               `json:"planning_strategy"`
	MaxIterations     int32                `json:"max_iterations"`
	Memory            AgentMemorySnapshot  `json:"memory"`
	LastExecutionAt   *time.Time           `json:"last_execution_at"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type ListAgentsResponse struct {
	Data []AgentDefinition `json:"data"`
}

// CreateAgentRequest defaults: status="active",
// planning_strategy="plan-act-observe", max_iterations=3.
type CreateAgentRequest struct {
	Name             string      `json:"name"`
	Description      *string     `json:"description,omitempty"`
	Status           *string     `json:"status,omitempty"`
	SystemPrompt     *string     `json:"system_prompt,omitempty"`
	Objective        *string     `json:"objective,omitempty"`
	ToolIDs          []uuid.UUID `json:"tool_ids,omitempty"`
	PlanningStrategy *string     `json:"planning_strategy,omitempty"`
	MaxIterations    *int32      `json:"max_iterations,omitempty"`
}

type UpdateAgentRequest struct {
	Name             *string              `json:"name,omitempty"`
	Description      *string              `json:"description,omitempty"`
	Status           *string              `json:"status,omitempty"`
	SystemPrompt     *string              `json:"system_prompt,omitempty"`
	Objective        *string              `json:"objective,omitempty"`
	ToolIDs          *[]uuid.UUID         `json:"tool_ids,omitempty"`
	PlanningStrategy *string              `json:"planning_strategy,omitempty"`
	MaxIterations    *int32               `json:"max_iterations,omitempty"`
	Memory           *AgentMemorySnapshot `json:"memory,omitempty"`
}

type ExecuteAgentRequest struct {
	UserMessage          string          `json:"user_message"`
	Objective            *string         `json:"objective"`
	KnowledgeBaseID      *uuid.UUID      `json:"knowledge_base_id"`
	PurposeJustification *string         `json:"purpose_justification,omitempty"`
	Context              json.RawMessage `json:"context,omitempty"`
}

type AgentExecutionResponse struct {
	AgentID        uuid.UUID             `json:"agent_id"`
	Steps          []AgentPlanStep       `json:"steps"`
	Traces         []AgentExecutionTrace `json:"traces"`
	FinalResponse  string                `json:"final_response"`
	UsedToolNames  []string              `json:"used_tool_names"`
	ExecutedAt     time.Time             `json:"executed_at"`
}

// Agent creation defaults exported for the HTTP-handler slice.
const (
	DefaultAgentStatus           = "active"
	DefaultAgentPlanningStrategy = "plan-act-observe"
	DefaultAgentMaxIterations int32 = 3
)
