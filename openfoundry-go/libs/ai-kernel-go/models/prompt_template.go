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
	InputVariables []string   `json:"input_variables"`
	Notes          string     `json:"notes"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedBy      *uuid.UUID `json:"created_by"`
}

// PromptTemplate carries every version + the convenience pointer
// at the latest one (current_version).
type PromptTemplate struct {
	ID                  uuid.UUID       `json:"id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	Category            string          `json:"category"`
	Status              string          `json:"status"`
	Tags                []string        `json:"tags"`
	LatestVersionNumber int32           `json:"latest_version_number"`
	CurrentVersion      PromptVersion   `json:"current_version"`
	Versions            []PromptVersion `json:"versions"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type ListPromptTemplatesResponse struct {
	Data []PromptTemplate `json:"data"`
}

// CreatePromptTemplateRequest defaults: category="copilot".
type CreatePromptTemplateRequest struct {
	Name           string   `json:"name"`
	Description    *string  `json:"description"`
	Category       *string  `json:"category"`
	Content        string   `json:"content"`
	InputVariables []string `json:"input_variables"`
	Tags           []string `json:"tags"`
	Notes          *string  `json:"notes"`
}

type UpdatePromptTemplateRequest struct {
	Name           *string   `json:"name"`
	Description    *string   `json:"description"`
	Category       *string   `json:"category"`
	Status         *string   `json:"status"`
	Content        *string   `json:"content"`
	InputVariables *[]string `json:"input_variables"`
	Tags           *[]string `json:"tags"`
	Notes          *string   `json:"notes"`
}

type RenderPromptRequest struct {
	Variables json.RawMessage `json:"variables"`
	Strict    bool            `json:"strict"`
}

type RenderPromptResponse struct {
	PromptID         uuid.UUID `json:"prompt_id"`
	VersionNumber    int32     `json:"version_number"`
	RenderedContent  string    `json:"rendered_content"`
	MissingVariables []string  `json:"missing_variables"`
}

const DefaultPromptCategory = "copilot"

func (r *CreatePromptTemplateRequest) UnmarshalJSON(data []byte) error {
	type alias CreatePromptTemplateRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	if r.Description == nil {
		r.Description = ptrOf("")
	}
	if r.Category == nil {
		r.Category = ptrOf(DefaultPromptCategory)
	}
	if r.InputVariables == nil {
		r.InputVariables = []string{}
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return nil
}

func (r *RenderPromptRequest) UnmarshalJSON(data []byte) error {
	type alias RenderPromptRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	r.Variables = defaultRawMessage(r.Variables, emptyJSONObject())
	return nil
}
