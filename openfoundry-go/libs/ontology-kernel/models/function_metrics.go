package models

import (
	"time"

	"github.com/google/uuid"
)

// FunctionPackageRun mirrors `struct FunctionPackageRun` in
// `libs/ontology-kernel/src/models/function_metrics.rs`.
type FunctionPackageRun struct {
	ID                     uuid.UUID  `json:"id"                       db:"id"`
	FunctionPackageID      uuid.UUID  `json:"function_package_id"      db:"function_package_id"`
	FunctionPackageName    string     `json:"function_package_name"    db:"function_package_name"`
	FunctionPackageVersion string     `json:"function_package_version" db:"function_package_version"`
	Runtime                string     `json:"runtime"                  db:"runtime"`
	Status                 string     `json:"status"                   db:"status"`
	InvocationKind         string     `json:"invocation_kind"          db:"invocation_kind"`
	ActionID               *uuid.UUID `json:"action_id"                db:"action_id"`
	ActionName             *string    `json:"action_name"              db:"action_name"`
	ObjectTypeID           *uuid.UUID `json:"object_type_id"           db:"object_type_id"`
	TargetObjectID         *uuid.UUID `json:"target_object_id"         db:"target_object_id"`
	ActorID                uuid.UUID  `json:"actor_id"                 db:"actor_id"`
	DurationMs             int64      `json:"duration_ms"              db:"duration_ms"`
	ErrorMessage           *string    `json:"error_message"            db:"error_message"`
	StartedAt              time.Time  `json:"started_at"               db:"started_at"`
	CompletedAt            time.Time  `json:"completed_at"             db:"completed_at"`
}

// ListFunctionPackageRunsQuery mirrors `struct ListFunctionPackageRunsQuery`.
type ListFunctionPackageRunsQuery struct {
	Page           *int64  `json:"page,omitempty"`
	PerPage        *int64  `json:"per_page,omitempty"`
	Status         *string `json:"status,omitempty"`
	InvocationKind *string `json:"invocation_kind,omitempty"`
}

// ListFunctionPackageRunsResponse mirrors `struct ListFunctionPackageRunsResponse`.
type ListFunctionPackageRunsResponse struct {
	Data    []FunctionPackageRun `json:"data"`
	Total   int64                `json:"total"`
	Page    int64                `json:"page"`
	PerPage int64                `json:"per_page"`
}

// FunctionPackageMetricsRow mirrors `struct FunctionPackageMetricsRow`.
type FunctionPackageMetricsRow struct {
	TotalRuns       int64      `db:"total_runs"`
	SuccessfulRuns  int64      `db:"successful_runs"`
	FailedRuns      int64      `db:"failed_runs"`
	SimulationRuns  int64      `db:"simulation_runs"`
	ActionRuns      int64      `db:"action_runs"`
	AvgDurationMs   *float64   `db:"avg_duration_ms"`
	P95DurationMs   *float64   `db:"p95_duration_ms"`
	MaxDurationMs   *int64     `db:"max_duration_ms"`
	LastRunAt       *time.Time `db:"last_run_at"`
	LastSuccessAt   *time.Time `db:"last_success_at"`
	LastFailureAt   *time.Time `db:"last_failure_at"`
}

// FunctionPackageMetricsResponse mirrors `struct FunctionPackageMetricsResponse`.
type FunctionPackageMetricsResponse struct {
	Package        FunctionPackageSummary `json:"package"`
	TotalRuns      int64                  `json:"total_runs"`
	SuccessfulRuns int64                  `json:"successful_runs"`
	FailedRuns     int64                  `json:"failed_runs"`
	SimulationRuns int64                  `json:"simulation_runs"`
	ActionRuns     int64                  `json:"action_runs"`
	SuccessRate    float64                `json:"success_rate"`
	AvgDurationMs  *float64               `json:"avg_duration_ms"`
	P95DurationMs  *float64               `json:"p95_duration_ms"`
	MaxDurationMs  *int64                 `json:"max_duration_ms"`
	LastRunAt      *time.Time             `json:"last_run_at"`
	LastSuccessAt  *time.Time             `json:"last_success_at"`
	LastFailureAt  *time.Time             `json:"last_failure_at"`
}
