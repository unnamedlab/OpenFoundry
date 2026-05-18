// Package function models synchronous and asynchronous function-mode
// invocations of a Compute Module (checklist CM.6 / CM.8).
//
// The types here are intentionally transport-agnostic: handlers map
// HTTP requests onto FunctionInvocation, the dispatcher executes the
// invocation against the module's runtime, and the repo persists the
// transitions. Sentinel errors are package-scoped so callers can drive
// errors.Is from any layer (handler → repo → dispatcher).
package function

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors. HTTP handlers map these to canonical status codes;
// the dispatcher and repo wrap them so errors.Is keeps working across
// layers.
var (
	// ErrFunctionNotFound is returned when no function with the given
	// name is registered on the target module. Maps to 404.
	ErrFunctionNotFound = errors.New("function: not found")

	// ErrModuleVersionInactive is returned when the module's active
	// version is missing or disabled and therefore cannot be invoked.
	// Maps to 409.
	ErrModuleVersionInactive = errors.New("function: module version is not active")

	// ErrInvocationTimeout is returned when the dispatcher's per-call
	// deadline elapses before the module replies. Maps to 504.
	ErrInvocationTimeout = errors.New("function: invocation timed out")

	// ErrPayloadTooLarge is returned when the inbound payload exceeds
	// the dispatcher's configured byte limit. Maps to 413.
	ErrPayloadTooLarge = errors.New("function: payload exceeds limit")

	// ErrInvocationNotFound is returned by the repo when an invocation
	// id has no matching record. Maps to 404.
	ErrInvocationNotFound = errors.New("function: invocation not found")

	// ErrInvocationTerminal is returned by Cancel when the invocation
	// has already reached a terminal status (succeeded/failed/cancelled/
	// timeout). Maps to 409.
	ErrInvocationTerminal = errors.New("function: invocation already terminal")
)

// Status is the lifecycle stamp on a FunctionInvocation.
//
// queued → running → {succeeded | failed | cancelled | timeout}
//
// Each terminal state freezes the row; further transitions are no-ops.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusTimeout   Status = "timeout"
)

// IsValid reports whether s is one of the canonical values.
func (s Status) IsValid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled, StatusTimeout:
		return true
	}
	return false
}

// IsTerminal reports whether s is a terminal status (no further
// transitions allowed).
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled, StatusTimeout:
		return true
	}
	return false
}

// FunctionInvocation is the persisted record of one invocation of a
// module function. The same struct represents both sync and async
// requests; only the handler decides whether to block on the result.
type FunctionInvocation struct {
	ID            uuid.UUID       `json:"id"`
	ModuleID      uuid.UUID       `json:"module_id"`
	ModuleVersion string          `json:"module_version,omitempty"`
	FunctionName  string          `json:"function_name"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	ActorID       uuid.UUID       `json:"actor_id"`
	ScheduledAt   time.Time       `json:"scheduled_at"`
	StartedAt     *time.Time      `json:"started_at,omitempty"`
	FinishedAt    *time.Time      `json:"finished_at,omitempty"`
	Status        Status          `json:"status"`
	Result        json.RawMessage `json:"result,omitempty"`
	ErrorMessage  string          `json:"error_message,omitempty"`
	CostUnits     int64           `json:"cost_units"`
}

// Clone returns a deep copy of the invocation safe to hand back to
// callers without aliasing the internal repo state.
func (f *FunctionInvocation) Clone() *FunctionInvocation {
	if f == nil {
		return nil
	}
	out := *f
	if len(f.Payload) > 0 {
		out.Payload = append(json.RawMessage(nil), f.Payload...)
	}
	if len(f.Result) > 0 {
		out.Result = append(json.RawMessage(nil), f.Result...)
	}
	if f.StartedAt != nil {
		t := *f.StartedAt
		out.StartedAt = &t
	}
	if f.FinishedAt != nil {
		t := *f.FinishedAt
		out.FinishedAt = &t
	}
	return &out
}
