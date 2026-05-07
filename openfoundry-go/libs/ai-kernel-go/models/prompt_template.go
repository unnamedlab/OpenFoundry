package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PromptVersion is one revision of a prompt template.
type PromptVersion struct {
	VersionNumber  int32      `json:"version_number"`
	Content        string     `json:"content"`
	InputVariables []string   `json:"input_variables,omitempty"`
	Notes          string     `json:"notes"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedBy      *uuid.UUID `json:"created_by"`
}

// PromptTemplate carries every version + the convenience pointer
// at the latest one (current_version).
type PromptTemplate struct {
	ID                   uuid.UUID       `json:"id"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	Category             string          `json:"category"`
	Status               string          `json:"status"`
	Tags                 []string        `json:"tags"`
	LatestVersionNumber  int32           `json:"latest_version_number"`
	CurrentVersion       PromptVersion   `json:"current_version"`
	Versions             []PromptVersion `json:"versions"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type ListPromptTemplatesResponse struct {
	Data []PromptTemplate `json:"data"`
}

// CreatePromptTemplateRequest defaults: category="copilot".
type CreatePromptTemplateRequest struct {
	Name           string   `json:"name"`
	Description    *string  `json:"description,omitempty"`
	Category       *string  `json:"category,omitempty"`
	Content        string   `json:"content"`
	InputVariables []string `json:"input_variables,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Notes          *string  `json:"notes,omitempty"`
}

type UpdatePromptTemplateRequest struct {
	Name           *string   `json:"name,omitempty"`
	Description    *string   `json:"description,omitempty"`
	Category       *string   `json:"category,omitempty"`
	Status         *string   `json:"status,omitempty"`
	Content        *string   `json:"content,omitempty"`
	InputVariables *[]string `json:"input_variables,omitempty"`
	Tags           *[]string `json:"tags,omitempty"`
	Notes          *string   `json:"notes,omitempty"`
}

type RenderPromptRequest struct {
	Variables json.RawMessage `json:"variables,omitempty"`
	Strict    bool            `json:"strict"`
}

type RenderPromptResponse struct {
	PromptID         uuid.UUID `json:"prompt_id"`
	VersionNumber    int32     `json:"version_number"`
	RenderedContent  string    `json:"rendered_content"`
	MissingVariables []string  `json:"missing_variables"`
}

const DefaultPromptCategory = "copilot"
