// Package models holds wire + persistence types for function-runtime-service.
//
// FunctionDefinition + FunctionVersion describe user-authored
// functions (TypeScript today, Python stubbed) sourced from
// code-repository-service. FunctionRun captures one execution
// attempt — synchronous or asynchronous — with its input / output and
// timing metadata.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Runtime identifies the language runtime used to execute a function.
type Runtime string

const (
	RuntimeTypeScript Runtime = "ts"
	RuntimePython     Runtime = "python"
)

// Valid reports whether r is one of the recognised runtimes.
func (r Runtime) Valid() bool {
	switch r {
	case RuntimeTypeScript, RuntimePython:
		return true
	}
	return false
}

// Status enumerates the lifecycle states of a FunctionDefinition.
type Status string

const (
	StatusDraft      Status = "draft"
	StatusActive     Status = "active"
	StatusDeprecated Status = "deprecated"
)

// Valid reports whether s is one of the recognised statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusActive, StatusDeprecated:
		return true
	}
	return false
}

// RunStatus enumerates the lifecycle states of a FunctionRun.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusTimeout   RunStatus = "timeout"
)

// Param is one input or output parameter declared in a Signature.
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

// Signature is the typed contract a function exposes.
type Signature struct {
	Inputs []Param `json:"inputs"`
	Output Param   `json:"output"`
}

// FunctionDefinition is the identity of a user-authored function.
// The mutable activation pointer lives on the row (ActiveVersion);
// the immutable source for each published version lives on
// FunctionVersion.
type FunctionDefinition struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	Namespace     string     `json:"namespace"`
	Name          string     `json:"name"`
	Runtime       Runtime    `json:"runtime"`
	Signature     Signature  `json:"signature"`
	Status        Status     `json:"status"`
	ActiveVersion *int       `json:"active_version,omitempty"`
	LatestVersion int        `json:"latest_version"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ActivatedAt   *time.Time `json:"activated_at,omitempty"`
}

// FunctionVersion is one published, immutable revision of a function.
// SourceURI points at the code-repository-service blob holding the
// transpiled / packaged sources EntryPoint references.
type FunctionVersion struct {
	ID         uuid.UUID `json:"id"`
	FunctionID uuid.UUID `json:"function_id"`
	Version    int       `json:"version"`
	SourceURI  string    `json:"source_uri"`
	EntryPoint string    `json:"entry_point"`
	CreatedAt  time.Time `json:"created_at"`
}

// FunctionRun captures one execution attempt. Output / DurationMs /
// FinishedAt are populated once Status reaches a terminal state.
type FunctionRun struct {
	ID              uuid.UUID       `json:"id"`
	FunctionID      uuid.UUID       `json:"function_id"`
	FunctionVersion int             `json:"function_version"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	ActorID         uuid.UUID       `json:"actor_id"`
	Status          RunStatus       `json:"status"`
	Input           json.RawMessage `json:"input"`
	Output          json.RawMessage `json:"output,omitempty"`
	Error           string          `json:"error,omitempty"`
	StartedAt       time.Time       `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	DurationMs      int64           `json:"duration_ms"`
}

// ─── HTTP request envelopes ───────────────────────────────────────────

// RegisterFunctionRequest is the body of POST /api/v1/functions.
type RegisterFunctionRequest struct {
	Namespace  string    `json:"namespace"`
	Name       string    `json:"name"`
	Runtime    Runtime   `json:"runtime"`
	Signature  Signature `json:"signature"`
	SourceURI  string    `json:"source_uri"`
	EntryPoint string    `json:"entry_point"`
}

// PublishVersionRequest is the body of POST /api/v1/functions/{id}/versions.
type PublishVersionRequest struct {
	SourceURI  string `json:"source_uri"`
	EntryPoint string `json:"entry_point"`
}

// InvokeRequest is the body of POST /api/v1/functions/{id}/invoke.
type InvokeRequest struct {
	// Version, when omitted, dispatches to the function's
	// ActiveVersion. Explicitly setting it is required when calling
	// a function whose ActiveVersion is nil (e.g. draft testing).
	Version *int            `json:"version,omitempty"`
	Input   json.RawMessage `json:"input"`
	// TimeoutSeconds, when set, overrides the executor's default.
	// Clamped to MaxExecutorTimeout server-side.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}
