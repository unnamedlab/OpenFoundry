package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Pipeline is the legacy `pipelines` table row.
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

// ParsedNodes decodes the DAG JSON into the typed PipelineNode slice.
func (p *Pipeline) ParsedNodes() ([]PipelineNode, error) {
	if len(p.DAG) == 0 {
		return nil, nil
	}
	var nodes []PipelineNode
	if err := json.Unmarshal(p.DAG, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// Schedule decodes the schedule_config JSON, falling back to the
// zero-value when decoding fails (matches the Rust `unwrap_or_default`).
func (p *Pipeline) Schedule() PipelineScheduleConfig {
	var s PipelineScheduleConfig
	_ = json.Unmarshal(p.ScheduleConfig, &s)
	return s
}

// ParsedRetryPolicy mirrors the Rust accessor + Default impl.
func (p *Pipeline) ParsedRetryPolicy() PipelineRetryPolicy {
	out := DefaultPipelineRetryPolicy()
	if len(p.RetryPolicy) > 0 {
		_ = json.Unmarshal(p.RetryPolicy, &out)
	}
	return out
}

// PipelineScheduleConfig is the parsed schedule_config JSON.
type PipelineScheduleConfig struct {
	Enabled bool    `json:"enabled"`
	Cron    *string `json:"cron,omitempty"`
}

// PipelineRetryPolicy is the parsed retry_policy JSON.
type PipelineRetryPolicy struct {
	MaxAttempts              uint32 `json:"max_attempts"`
	RetryOnFailure           bool   `json:"retry_on_failure"`
	AllowPartialReexecution  bool   `json:"allow_partial_reexecution"`
}

// DefaultPipelineRetryPolicy mirrors the Rust `Default::default()`.
func DefaultPipelineRetryPolicy() PipelineRetryPolicy {
	return PipelineRetryPolicy{
		MaxAttempts:             1,
		RetryOnFailure:          false,
		AllowPartialReexecution: true,
	}
}

// PipelineColumnMapping is one entry under `node.config.column_mappings`.
type PipelineColumnMapping struct {
	SourceDatasetID *uuid.UUID `json:"source_dataset_id,omitempty"`
	SourceColumn    string     `json:"source_column"`
	TargetColumn    string     `json:"target_column"`
}

// PipelineNode is one node of the pipeline DAG.
type PipelineNode struct {
	ID                string          `json:"id"`
	Label             string          `json:"label"`
	TransformType     string          `json:"transform_type"`
	Config            json.RawMessage `json:"config,omitempty"`
	DependsOn         []string        `json:"depends_on,omitempty"`
	InputDatasetIDs   []uuid.UUID     `json:"input_dataset_ids,omitempty"`
	OutputDatasetID   *uuid.UUID      `json:"output_dataset_id,omitempty"`
}

// ColumnMappings extracts the column-mapping list embedded in the
// node's config blob.
func (n *PipelineNode) ColumnMappings() []PipelineColumnMapping {
	if len(n.Config) == 0 {
		return nil
	}
	var holder struct {
		Mappings []PipelineColumnMapping `json:"column_mappings"`
	}
	if err := json.Unmarshal(n.Config, &holder); err != nil {
		return nil
	}
	return holder.Mappings
}

// CreatePipelineRequest is the JSON body for `POST /api/v1/pipelines`.
type CreatePipelineRequest struct {
	Name           string                  `json:"name"`
	Description    *string                 `json:"description,omitempty"`
	Status         *string                 `json:"status,omitempty"`
	Nodes          []PipelineNode          `json:"nodes"`
	ScheduleConfig *PipelineScheduleConfig `json:"schedule_config,omitempty"`
	RetryPolicy    *PipelineRetryPolicy    `json:"retry_policy,omitempty"`
}

// UpdatePipelineRequest is the JSON body for `PATCH /api/v1/pipelines/{id}`.
type UpdatePipelineRequest struct {
	Name           *string                 `json:"name,omitempty"`
	Description    *string                 `json:"description,omitempty"`
	Status         *string                 `json:"status,omitempty"`
	Nodes          *[]PipelineNode         `json:"nodes,omitempty"`
	ScheduleConfig *PipelineScheduleConfig `json:"schedule_config,omitempty"`
	RetryPolicy    *PipelineRetryPolicy    `json:"retry_policy,omitempty"`
}

// ListPipelinesQuery is the URL query for `GET /api/v1/pipelines`.
type ListPipelinesQuery struct {
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
	Search  *string `json:"search,omitempty"`
	Status  *string `json:"status,omitempty"`
}

// ListPipelinesResponse is the envelope for the list endpoint.
type ListPipelinesResponse struct {
	Data    []Pipeline `json:"data"`
	Total   int64      `json:"total"`
	Page    int64      `json:"page"`
	PerPage int64      `json:"per_page"`
}
