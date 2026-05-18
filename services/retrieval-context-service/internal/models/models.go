// Package models holds wire-format DTOs for retrieval-context-service.
//
// The service owns the document-intelligence sub-domain (extraction
// jobs, status events, structured extractions). Wire shape is hand-
// rolled because proto/ai/{agent,copilot,prompt,rag}.proto are
// intentionally empty stubs — schema source of truth is the migration
// in internal/repo/migrations/.
package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// JobStatus enumerates the lifecycle of a document-intelligence job.
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// IsValid reports whether s is a known status token.
func (s JobStatus) IsValid() bool {
	switch s {
	case JobStatusQueued, JobStatusRunning, JobStatusSucceeded, JobStatusFailed, JobStatusCancelled:
		return true
	}
	return false
}

// NormalizeJobStatus returns the lower-case form, trimmed, ready for
// IsValid checks. Empty input maps to the empty JobStatus.
func NormalizeJobStatus(s string) JobStatus {
	return JobStatus(strings.ToLower(strings.TrimSpace(s)))
}

// Job is one document-intelligence run.
type Job struct {
	ID        uuid.UUID       `json:"id"`
	SourceURI string          `json:"source_uri"`
	MimeType  *string         `json:"mime_type,omitempty"`
	Pipeline  string          `json:"pipeline"`
	Status    JobStatus       `json:"status"`
	Options   json.RawMessage `json:"options"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// StatusEvent records a transition for a job (one row in
// document_intelligence_status_events).
type StatusEvent struct {
	ID        uuid.UUID `json:"id"`
	JobID     uuid.UUID `json:"job_id"`
	Status    JobStatus `json:"status"`
	Message   *string   `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Extraction is one structured artefact produced by the pipeline (text,
// table, image OCR, …) keyed by extraction_kind.
type Extraction struct {
	ID             uuid.UUID       `json:"id"`
	JobID          uuid.UUID       `json:"job_id"`
	ExtractionKind string          `json:"extraction_kind"`
	Payload        json.RawMessage `json:"payload"`
	Confidence     *float32        `json:"confidence,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// CreateJobRequest is the body of POST /api/v1/document-intelligence/jobs.
type CreateJobRequest struct {
	SourceURI string          `json:"source_uri"`
	MimeType  *string         `json:"mime_type,omitempty"`
	Pipeline  string          `json:"pipeline"`
	Status    *JobStatus      `json:"status,omitempty"`
	Options   json.RawMessage `json:"options,omitempty"`
}

// UpdateJobRequest is the body of PATCH /api/v1/document-intelligence/jobs/{id}.
// Every field is optional; only the supplied ones are written.
type UpdateJobRequest struct {
	Status   *JobStatus      `json:"status,omitempty"`
	MimeType *string         `json:"mime_type,omitempty"`
	Options  json.RawMessage `json:"options,omitempty"`
}

// AppendEventRequest is the body of POST /jobs/{id}/events.
type AppendEventRequest struct {
	Status  JobStatus `json:"status"`
	Message *string   `json:"message,omitempty"`
}

// RecordExtractionRequest is the body of POST /jobs/{id}/extractions.
type RecordExtractionRequest struct {
	ExtractionKind string          `json:"extraction_kind"`
	Payload        json.RawMessage `json:"payload"`
	Confidence     *float32        `json:"confidence,omitempty"`
}

// ListJobsResponse is the cursor-paginated job list envelope.
type ListJobsResponse struct {
	Data       []Job   `json:"data"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

// ListEventsResponse is the (non-paginated) event envelope per job.
type ListEventsResponse struct {
	Data []StatusEvent `json:"data"`
}

// ListExtractionsResponse is the (non-paginated) extraction envelope per job.
type ListExtractionsResponse struct {
	Data []Extraction `json:"data"`
}
