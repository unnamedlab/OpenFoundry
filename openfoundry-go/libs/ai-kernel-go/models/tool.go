package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ToolDefinition is the catalog row for one tool the agent runtime
// can dispatch to.
type ToolDefinition struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Category        string          `json:"category"`
	ExecutionMode   string          `json:"execution_mode"`
	ExecutionConfig json.RawMessage `json:"execution_config"`
	Status          string          `json:"status"`
	InputSchema     json.RawMessage `json:"input_schema"`
	OutputSchema    json.RawMessage `json:"output_schema"`
	Tags            []string        `json:"tags"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ListToolsResponse struct {
	Data []ToolDefinition `json:"data"`
}

// CreateToolRequest defaults match Rust serde:
// category="analysis", execution_mode="simulated",
// execution_config={}, status="active", input_schema={},
// output_schema={}.
type CreateToolRequest struct {
	Name            string           `json:"name"`
	Description     *string          `json:"description"`
	Category        *string          `json:"category"`
	ExecutionMode   *string          `json:"execution_mode"`
	ExecutionConfig *json.RawMessage `json:"execution_config"`
	Status          *string          `json:"status"`
	InputSchema     *json.RawMessage `json:"input_schema"`
	OutputSchema    *json.RawMessage `json:"output_schema"`
	Tags            []string         `json:"tags"`
}

type UpdateToolRequest struct {
	Name            *string          `json:"name"`
	Description     *string          `json:"description"`
	Category        *string          `json:"category"`
	ExecutionMode   *string          `json:"execution_mode"`
	ExecutionConfig *json.RawMessage `json:"execution_config"`
	Status          *string          `json:"status"`
	InputSchema     *json.RawMessage `json:"input_schema"`
	OutputSchema    *json.RawMessage `json:"output_schema"`
	Tags            *[]string        `json:"tags"`
}

const (
	DefaultToolCategory      = "analysis"
	DefaultToolExecutionMode = "simulated"
	DefaultToolStatus        = "active"
)

// SupportedExecutionModes mirrors Rust supported_execution_modes()
// verbatim. Order is preserved for stable display in clients.
func SupportedExecutionModes() []string {
	return []string{
		"simulated",
		"http_json",
		"openfoundry_api",
		"native_sql",
		"native_dataset",
		"native_ontology",
		"native_pipeline",
		"native_report",
		"native_workflow",
		"native_code_repo",
		"knowledge_search",
	}
}

// ValidateExecutionMode returns true when mode is one of the
// supported execution modes (case-insensitive). Mirrors Rust
// validate_execution_mode in handlers/tools.rs.
func ValidateExecutionMode(mode string) bool {
	for _, candidate := range SupportedExecutionModes() {
		if strings.EqualFold(candidate, mode) {
			return true
		}
	}
	return false
}

func (r *CreateToolRequest) UnmarshalJSON(data []byte) error {
	type alias CreateToolRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	if r.Description == nil {
		r.Description = ptrOf("")
	}
	if r.Category == nil {
		r.Category = ptrOf(DefaultToolCategory)
	}
	if r.ExecutionMode == nil {
		r.ExecutionMode = ptrOf(DefaultToolExecutionMode)
	}
	if r.ExecutionConfig == nil {
		v := emptyJSONObject()
		r.ExecutionConfig = &v
	}
	if r.Status == nil {
		r.Status = ptrOf(DefaultToolStatus)
	}
	if r.InputSchema == nil {
		v := emptyJSONObject()
		r.InputSchema = &v
	}
	if r.OutputSchema == nil {
		v := emptyJSONObject()
		r.OutputSchema = &v
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return nil
}
