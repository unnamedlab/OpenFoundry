// Package state holds the pure JobStatus state machine.
// The Postgres-backed JobRepo lands in a follow-up slice.
package state

import "fmt"

// JobStatus is the lifecycle of a row in reindex_coordinator.reindex_jobs.
// Mirrors the legacy Go worker's Status enum.
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// AllStatuses lists the wire-format tokens, also used by the
// SQL CHECK constraint. Tests pin this against the migration.
var AllStatuses = []JobStatus{
	StatusQueued, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled,
}

// ParseStatus is the inverse of String().
func ParseStatus(v string) (JobStatus, error) {
	for _, s := range AllStatuses {
		if string(s) == v {
			return s, nil
		}
	}
	return "", &UnknownStatusError{Value: v}
}

// String returns the wire-format token.
func (s JobStatus) String() string { return string(s) }

// IsTerminal returns true for completed / failed / cancelled.
func (s JobStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

// CanTransitionTo validates a proposed transition. Allowed moves:
//
//	Queued  → Running | Cancelled | Failed
//	Running → Completed | Failed | Cancelled
//	* → *  (idempotent self-loop on terminal states is allowed)
//
// Terminal → non-terminal is forbidden.
func (s JobStatus) CanTransitionTo(next JobStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case StatusQueued:
		return next == StatusRunning || next == StatusFailed || next == StatusCancelled
	case StatusRunning:
		return next == StatusCompleted || next == StatusFailed || next == StatusCancelled
	default:
		return false
	}
}

// EnsureTransition is the typed-error variant of CanTransitionTo.
func (s JobStatus) EnsureTransition(next JobStatus) error {
	if s.CanTransitionTo(next) {
		return nil
	}
	return &IllegalTransitionError{From: s, To: next}
}

// --- errors --------------------------------------------------------------

// UnknownStatusError is returned by ParseStatus for unknown tokens.
type UnknownStatusError struct{ Value string }

func (e *UnknownStatusError) Error() string {
	return fmt.Sprintf("state: unknown status %q", e.Value)
}

// IllegalTransitionError is returned when CanTransitionTo would be false.
type IllegalTransitionError struct {
	From JobStatus
	To   JobStatus
}

func (e *IllegalTransitionError) Error() string {
	return fmt.Sprintf("state: illegal transition %s → %s", e.From, e.To)
}
