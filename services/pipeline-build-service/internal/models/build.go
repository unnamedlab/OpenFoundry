// Package models hosts persistent shapes for pipeline-build-service.
//
// Field order, JSON tags and the per-state string vocabulary all match
// `services/pipeline-build-service/src/models/{build,job,pipeline,run}.rs`
// 1:1 so `proto/pipeline/builds.proto` round-trips through either
// language unchanged.
package models

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// BuildState mirrors the Foundry Build state machine. The string
// values are the canonical wire form persisted in `builds.state` and
// re-used by the legacy `pipeline_runs.status` column.
type BuildState string

const (
	BuildResolution BuildState = "BUILD_RESOLUTION"
	BuildQueued     BuildState = "BUILD_QUEUED"
	BuildRunning    BuildState = "BUILD_RUNNING"
	BuildAborting   BuildState = "BUILD_ABORTING"
	BuildFailed     BuildState = "BUILD_FAILED"
	BuildAborted    BuildState = "BUILD_ABORTED"
	BuildCompleted  BuildState = "BUILD_COMPLETED"
)

// AllBuildStates lists every valid BuildState — useful for SQL CHECK
// validation and unit-test exhaustiveness.
var AllBuildStates = []BuildState{
	BuildResolution, BuildQueued, BuildRunning, BuildAborting,
	BuildFailed, BuildAborted, BuildCompleted,
}

// IsTerminal mirrors the Rust `BuildState::is_terminal`.
func (s BuildState) IsTerminal() bool {
	return s == BuildFailed || s == BuildAborted || s == BuildCompleted
}

// ParseBuildState converts a wire string to a typed BuildState.
func ParseBuildState(s string) (BuildState, error) {
	for _, candidate := range AllBuildStates {
		if string(candidate) == s {
			return candidate, nil
		}
	}
	return "", &UnknownBuildState{Value: s}
}

// UnknownBuildState is returned by ParseBuildState on unknown input.
type UnknownBuildState struct{ Value string }

func (e *UnknownBuildState) Error() string { return "unknown build state: " + e.Value }

// AbortPolicy mirrors Foundry "Builds.md § Job execution".
type AbortPolicy string

const (
	AbortDependentOnly   AbortPolicy = "DEPENDENT_ONLY"
	AbortAllNonDependent AbortPolicy = "ALL_NON_DEPENDENT"
)

// AllAbortPolicies lists every valid abort policy.
var AllAbortPolicies = []AbortPolicy{AbortDependentOnly, AbortAllNonDependent}

// ParseAbortPolicy converts a wire string to a typed AbortPolicy. The
// fallback to AbortDependentOnly matches the Rust `Default` impl.
func ParseAbortPolicy(s string) AbortPolicy {
	for _, candidate := range AllAbortPolicies {
		if string(candidate) == s {
			return candidate
		}
	}
	return AbortDependentOnly
}

// Build is the concrete row shape for the `builds` table.
type Build struct {
	ID                uuid.UUID  `json:"id"`
	RID               string     `json:"rid"`
	PipelineRID       string     `json:"pipeline_rid"`
	BuildBranch       string     `json:"build_branch"`
	JobSpecFallback   []string   `json:"job_spec_fallback"`
	State             string     `json:"state"`
	TriggerKind       string     `json:"trigger_kind"`
	ForceBuild        bool       `json:"force_build"`
	AbortPolicy       string     `json:"abort_policy"`
	QueuedAt          *time.Time `json:"queued_at,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	ErrorMessage      *string    `json:"error_message,omitempty"`
	RequestedBy       string     `json:"requested_by"`
	CreatedAt         time.Time  `json:"created_at"`
}

// BuildState projects the string column to a typed value.
func (b *Build) BuildState() (BuildState, error) {
	return ParseBuildState(b.State)
}

// ParsedAbortPolicy projects the string column with the same fallback
// the Rust accessor uses.
func (b *Build) ParsedAbortPolicy() AbortPolicy {
	return ParseAbortPolicy(b.AbortPolicy)
}

// CreateBuildRequest is the JSON body for `POST /api/v1/builds`.
type CreateBuildRequest struct {
	PipelineRID       string       `json:"pipeline_rid"`
	BuildBranch       string       `json:"build_branch"`
	JobSpecFallback   []string     `json:"job_spec_fallback,omitempty"`
	ForceBuild        bool         `json:"force_build,omitempty"`
	OutputDatasetRIDs []string     `json:"output_dataset_rids,omitempty"`
	TriggerKind       *string      `json:"trigger_kind,omitempty"`
	AbortPolicy       *AbortPolicy `json:"abort_policy,omitempty"`
}

// ListBuildsQuery is the URL query for `GET /api/v1/builds`.
type ListBuildsQuery struct {
	Branch      string     `json:"branch,omitempty"`
	Status      string     `json:"status,omitempty"`
	PipelineRID string     `json:"pipeline_rid,omitempty"`
	Since       *time.Time `json:"since,omitempty"`
	Cursor      string     `json:"cursor,omitempty"`
	Limit       *int64     `json:"limit,omitempty"`
}

// BuildEnvelope wraps the build with its jobs (mirrors the Rust shape).
type BuildEnvelope struct {
	Build
	Jobs []Job `json:"jobs"`
}

// EncodeStringSlice is the SQL-friendly serialisation for
// pgx's text[] parameter binding. (sqlx uses Vec<String> directly.)
func EncodeStringSlice(s []string) ([]byte, error) {
	if s == nil {
		return json.Marshal([]string{})
	}
	return json.Marshal(s)
}

// DecodeStringSlice undoes EncodeStringSlice.
func DecodeStringSlice(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty string slice payload")
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
