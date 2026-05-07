package models

import (
	"time"

	"github.com/google/uuid"
)

// JobState mirrors Foundry "Builds.md § Job states" verbatim.
type JobState string

const (
	JobWaiting      JobState = "WAITING"
	JobRunPending   JobState = "RUN_PENDING"
	JobRunning      JobState = "RUNNING"
	JobAbortPending JobState = "ABORT_PENDING"
	JobAborted      JobState = "ABORTED"
	JobFailed       JobState = "FAILED"
	JobCompleted    JobState = "COMPLETED"
)

// AllJobStates lists every valid JobState — matches the Rust ALL slice.
var AllJobStates = []JobState{
	JobWaiting, JobRunPending, JobRunning, JobAbortPending,
	JobAborted, JobFailed, JobCompleted,
}

// IsTerminal mirrors the Rust `JobState::is_terminal`.
func (s JobState) IsTerminal() bool {
	return s == JobAborted || s == JobFailed || s == JobCompleted
}

// UnknownJobState is returned by ParseJobState on unknown input.
type UnknownJobState struct{ Value string }

func (e *UnknownJobState) Error() string { return "unknown job state: " + e.Value }

// ParseJobState converts a wire string to a typed JobState.
func ParseJobState(s string) (JobState, error) {
	for _, candidate := range AllJobStates {
		if string(candidate) == s {
			return candidate, nil
		}
	}
	return "", &UnknownJobState{Value: s}
}

// Job is the row shape for the `jobs` table.
type Job struct {
	ID                    uuid.UUID `json:"id"`
	RID                   string    `json:"rid"`
	BuildID               uuid.UUID `json:"build_id"`
	JobSpecRID            string    `json:"job_spec_rid"`
	State                 string    `json:"state"`
	OutputTransactionRIDs []string  `json:"output_transaction_rids"`
	StateChangedAt        time.Time `json:"state_changed_at"`
	Attempt               int32     `json:"attempt"`
	StaleSkipped          bool      `json:"stale_skipped"`
	FailureReason         *string   `json:"failure_reason,omitempty"`
	OutputContentHash     *string   `json:"output_content_hash,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}

// JobState projects the string column to a typed value.
func (j *Job) JobState() (JobState, error) { return ParseJobState(j.State) }
