// Package runners ports
// `services/pipeline-build-service/src/domain/runners/*` — the five
// per-kind job runners (SYNC, TRANSFORM, HEALTH_CHECK, ANALYTICAL,
// EXPORT) plus the dispatcher routing JobContext → JobRunner.
//
// **Phase B scope**:
//
//   - JobRunner interface + JobOutcome union + JobContext.
//   - DispatchingRunner that maps `JobSpec.LogicKind` to one of five
//     concrete runners.
//   - `LogicKinds` constants + `IsKnown` helper.
//   - `ValidateLogicKind` arity check.
//   - Skeleton runners that surface `runner_not_wired:<kind>` until
//     the HTTP / engine wiring lands. Each skeleton matches the Rust
//     trait shape exactly so the parallel orchestrator (Phase B
//     `build_executor`) can drive them end-to-end without the
//     production runtime.
//
// Phase B deliberately ports the lifecycle structure (state machine,
// failure cascade, parallel scheduler) without the concrete runtime
// each kind would call into — those live in their own service-client
// phases (connector-management for SYNC, engine for TRANSFORM,
// dataset-quality for HEALTH_CHECK, ai-service for ANALYTICAL,
// export-target HTTP for EXPORT).
package runners

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// LogSink mirrors the Rust `crate::domain::logs::LogSink` trait used
// by runners to fan out live-log entries to the build's SSE consumers.
// Concrete implementations land alongside the live-logs port; today
// nil is the only valid value.
type LogSink interface {
	Append(jobID uuid.UUID, line string)
}

// ── Logic kinds ─────────────────────────────────────────────────────

// LogicKind values matched against `JobSpec.LogicKind`.
const (
	LogicKindSync        = "SYNC"
	LogicKindTransform   = "TRANSFORM"
	LogicKindHealthCheck = "HEALTH_CHECK"
	LogicKindAnalytical  = "ANALYTICAL"
	LogicKindExport      = "EXPORT"
)

// AllLogicKinds mirrors `logic_kinds::ALL`.
var AllLogicKinds = []string{
	LogicKindSync, LogicKindTransform, LogicKindHealthCheck,
	LogicKindAnalytical, LogicKindExport,
}

// IsKnownLogicKind mirrors `logic_kinds::is_known`.
func IsKnownLogicKind(kind string) bool {
	for _, k := range AllLogicKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// ValidateLogicKind mirrors `pub fn validate_logic_kind`. Returns the
// canonical Foundry error messages the Rust crate uses so the build-
// resolution gate keeps round-tripping.
func ValidateLogicKind(kind string, outputCount int) error {
	switch kind {
	case LogicKindSync:
		if outputCount == 0 {
			return fmt.Errorf("SYNC job must declare at least one output dataset")
		}
	case LogicKindTransform:
		if outputCount == 0 {
			return fmt.Errorf("TRANSFORM job must declare at least one output dataset")
		}
	case LogicKindHealthCheck:
		if outputCount != 1 {
			return fmt.Errorf("HEALTH_CHECK job must declare exactly one output dataset (got %d)", outputCount)
		}
	case LogicKindAnalytical:
		if outputCount != 1 {
			return fmt.Errorf("ANALYTICAL job must declare exactly one output dataset (got %d)", outputCount)
		}
	case LogicKindExport:
		// EXPORT pushes data outside Foundry; output is optional.
	default:
		return fmt.Errorf("unknown logic_kind: %s", kind)
	}
	return nil
}

// ── JobOutcome / JobContext / JobRunner ────────────────────────────

// JobOutcomeKind discriminates the JobOutcome union (matches the Rust
// `enum JobOutcome` variants exactly).
type JobOutcomeKind int

const (
	JobOutcomeCompleted JobOutcomeKind = iota
	JobOutcomeFailed
)

// JobOutcome mirrors `pub enum JobOutcome`. One of the union variants
// is populated based on `Kind`.
type JobOutcome struct {
	Kind              JobOutcomeKind
	OutputContentHash string // populated when Kind == JobOutcomeCompleted
	Reason            string // populated when Kind == JobOutcomeFailed
}

// Completed builds a successful outcome carrying the deterministic
// content hash the executor commits with.
func Completed(hash string) JobOutcome {
	return JobOutcome{Kind: JobOutcomeCompleted, OutputContentHash: hash}
}

// Failed builds a failure outcome carrying the canonical error
// reason. Mirrors `JobOutcome::Failed { reason }`.
func Failed(reason string) JobOutcome {
	return JobOutcome{Kind: JobOutcomeFailed, Reason: reason}
}

// JobSpec is the slimmed shape the Rust `JobSpec` carries that this
// package needs. Real builds enrich it via `build_resolution` (Phase
// B's resolver port lands separately).
type JobSpec struct {
	JobSpecRID  string         `json:"job_spec_rid"`
	LogicKind   string         `json:"logic_kind"`
	OutputDatasetRIDs []string `json:"output_dataset_rids,omitempty"`
	// Config is the kind-specific JSON payload (SyncConfig,
	// AnalyticalConfig, ExportConfig, HealthCheckConfig, …) the
	// matching runner decodes.
	Config []byte `json:"config,omitempty"`
}

// ResolvedInputView mirrors `pub struct ResolvedInputView`. Slimmed
// to the fields runners read.
type ResolvedInputView struct {
	JobSpecRID            string     `json:"job_spec_rid"`
	DatasetRID            string     `json:"dataset_rid"`
	View                  string     `json:"view"`
	BranchID              uuid.UUID  `json:"branch_id"`
	TransactionRID        *string    `json:"transaction_rid,omitempty"`
	IsCircularDependency  bool       `json:"is_circular_dependency"`
}

// JobContext mirrors `pub struct JobContext`. The runtime wires up
// the executor's log-sink reference; tests pass nil to fall back to
// the standard logger.
type JobContext struct {
	BuildID        uuid.UUID
	BuildBranch    string
	JobID          uuid.UUID
	JobSpec        JobSpec
	ResolvedInputs []ResolvedInputView
	ForceBuild     bool
	LogSink        LogSink // optional; nil means use stdlib log
}

// JobRunner mirrors `#[async_trait] pub trait JobRunner`. The single
// `Run` method drives one job to a terminal `JobOutcome`.
type JobRunner interface {
	Run(ctx context.Context, jc *JobContext) JobOutcome
}

// ── DispatchingRunner ──────────────────────────────────────────────

// DispatchingRunner mirrors `pub struct DispatchingRunner`. Routes
// each JobContext to the kind-specific runner; unknown logic_kinds
// fail fast with a `Failed` outcome (matches the Rust default arm).
type DispatchingRunner struct {
	Sync        JobRunner
	Transform   JobRunner
	HealthCheck JobRunner
	Analytical  JobRunner
	Export      JobRunner
}

// Run mirrors `JobRunner for DispatchingRunner`.
func (d *DispatchingRunner) Run(ctx context.Context, jc *JobContext) JobOutcome {
	switch jc.JobSpec.LogicKind {
	case LogicKindSync:
		return runOrFallback(ctx, d.Sync, jc, "sync")
	case LogicKindTransform:
		return runOrFallback(ctx, d.Transform, jc, "transform")
	case LogicKindHealthCheck:
		return runOrFallback(ctx, d.HealthCheck, jc, "health_check")
	case LogicKindAnalytical:
		return runOrFallback(ctx, d.Analytical, jc, "analytical")
	case LogicKindExport:
		return runOrFallback(ctx, d.Export, jc, "export")
	default:
		return Failed(fmt.Sprintf("unknown logic_kind: %s", jc.JobSpec.LogicKind))
	}
}

func runOrFallback(ctx context.Context, runner JobRunner, jc *JobContext, kind string) JobOutcome {
	if runner == nil {
		return Failed(fmt.Sprintf("runner_not_wired:%s", kind))
	}
	return runner.Run(ctx, jc)
}

// ── Skeleton runners (Phase B placeholders) ────────────────────────

// SyncJobRunner mirrors `pub struct SyncJobRunner`. Runs a Foundry
// SYNC job by calling connector-management-service's ingest endpoint.
// The HTTP wiring lands in the connector-client phase; today we
// surface `runner_not_wired:sync`.
type SyncJobRunner struct {
	ConnectorBaseURL string
}

// NewSyncJobRunner mirrors `SyncJobRunner::new`.
func NewSyncJobRunner(baseURL string) *SyncJobRunner {
	return &SyncJobRunner{ConnectorBaseURL: baseURL}
}

// Run mirrors the SYNC runner.
func (r *SyncJobRunner) Run(_ context.Context, _ *JobContext) JobOutcome {
	return Failed("runner_not_wired:sync")
}

// TransformJobRunner mirrors `pub struct TransformJobRunner`.
// Delegates to the engine package once the runtime ports land.
type TransformJobRunner struct{}

// Run mirrors the TRANSFORM runner.
func (r *TransformJobRunner) Run(_ context.Context, _ *JobContext) JobOutcome {
	return Failed("runner_not_wired:transform")
}

// HealthCheckJobRunner mirrors `pub struct HealthCheckJobRunner`.
// Posts a check result to dataset-quality-service.
type HealthCheckJobRunner struct {
	QualityBaseURL string
}

// NewHealthCheckJobRunner mirrors `HealthCheckJobRunner::new`.
func NewHealthCheckJobRunner(baseURL string) *HealthCheckJobRunner {
	return &HealthCheckJobRunner{QualityBaseURL: baseURL}
}

// Run mirrors the HEALTH_CHECK runner.
func (r *HealthCheckJobRunner) Run(_ context.Context, _ *JobContext) JobOutcome {
	return Failed("runner_not_wired:health_check")
}

// AnalyticalJobRunner mirrors `pub struct AnalyticalJobRunner`.
// Materialises an object-set query to the output dataset.
type AnalyticalJobRunner struct{}

// Run mirrors the ANALYTICAL runner.
func (r *AnalyticalJobRunner) Run(_ context.Context, _ *JobContext) JobOutcome {
	return Failed("runner_not_wired:analytical")
}

// ExportJobRunner mirrors `pub struct ExportJobRunner`. Pushes the
// input dataset to an external destination (S3/GCS/HTTP/JDBC).
type ExportJobRunner struct{}

// Run mirrors the EXPORT runner.
func (r *ExportJobRunner) Run(_ context.Context, _ *JobContext) JobOutcome {
	return Failed("runner_not_wired:export")
}

// ── Build orchestrator (Phase B simplified parallel scheduler) ─────

// BuildOrchestrator mirrors the slimmest path of
// `build_executor::execute_build`. Drives a list of jobs to terminal
// outcomes via a worker pool sized by `parallelism`, honouring the
// dependency graph (`JobSpec.JobSpecRID` → list of dependency RIDs).
//
// A failed job stops scheduling new work but lets in-flight jobs
// finish — matches the Foundry "abort dependents" semantics. The
// `AbortPolicy` knob (abort-all-on-failure vs abort-dependents-only)
// lives on the call site so the orchestrator stays generic.
type BuildOrchestrator struct {
	Runner      JobRunner
	Parallelism int
	AbortAll    bool
}

// JobSpecWithDeps wraps a JobSpec with its dependency RIDs so the
// orchestrator can compute the ready-set without re-resolving the
// build.
type JobSpecWithDeps struct {
	Spec    JobSpec
	Depends []string
}

// JobResult pairs a JobSpecRID with its terminal outcome.
type JobResult struct {
	JobSpecRID string
	Outcome    JobOutcome
}

// Run executes a slice of job specs with dependency ordering.
//
// Mirrors the executor's "ready-queue" behaviour:
//
//   - Pick every job whose dependencies are all completed.
//   - Run up to `Parallelism` of them concurrently (semaphore-bounded).
//   - When a job fails, mark every transitively-dependent job as
//     `cancelled` (or every still-pending job when AbortAll is set).
//   - When a job completes, re-run the ready-set check.
//
// Crucially: only `parallelism` jobs may be in flight at any moment,
// so cancellation introduced by an in-flight failure lands BEFORE the
// next batch of ready jobs is dispatched.
func (o *BuildOrchestrator) Run(ctx context.Context, jobs []JobSpecWithDeps) []JobResult {
	if o.Runner == nil {
		out := make([]JobResult, len(jobs))
		for i, j := range jobs {
			out[i] = JobResult{JobSpecRID: j.Spec.JobSpecRID, Outcome: Failed("orchestrator_runner_unset")}
		}
		return out
	}
	parallelism := o.Parallelism
	if parallelism < 1 {
		parallelism = 1
	}

	type jobState struct {
		spec    JobSpec
		depends map[string]struct{}
		status  string // "pending", "running", "completed", "failed", "cancelled"
		outcome JobOutcome
	}
	state := map[string]*jobState{}
	order := []string{}
	for _, j := range jobs {
		s := &jobState{spec: j.Spec, depends: map[string]struct{}{}, status: "pending"}
		for _, d := range j.Depends {
			s.depends[d] = struct{}{}
		}
		state[j.Spec.JobSpecRID] = s
		order = append(order, j.Spec.JobSpecRID)
	}

	dependents := map[string][]string{}
	for rid, s := range state {
		for dep := range s.depends {
			dependents[dep] = append(dependents[dep], rid)
		}
	}

	resultsCh := make(chan JobResult, parallelism)
	var stateMu sync.Mutex
	inflight := 0
	failureSeen := false

	// nextReady picks one ready job under the lock, marks it
	// "running" and returns its rid (or "" when none ready).
	// Cancellation in AbortAll mode lands here so the in-flight
	// failure has a chance to flip `failureSeen` first.
	nextReady := func() string {
		stateMu.Lock()
		defer stateMu.Unlock()
		for _, rid := range order {
			s := state[rid]
			if s.status != "pending" {
				continue
			}
			if failureSeen && o.AbortAll {
				s.status = "cancelled"
				s.outcome = Failed("aborted")
				continue
			}
			ready := true
			for dep := range s.depends {
				ds := state[dep]
				if ds == nil || ds.status != "completed" {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			s.status = "running"
			inflight++
			return rid
		}
		return ""
	}

	finish := func(r JobResult) {
		stateMu.Lock()
		defer stateMu.Unlock()
		s := state[r.JobSpecRID]
		s.outcome = r.Outcome
		if r.Outcome.Kind == JobOutcomeCompleted {
			s.status = "completed"
		} else {
			s.status = "failed"
			failureSeen = true
			stack := []string{r.JobSpecRID}
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				for _, child := range dependents[top] {
					cs := state[child]
					if cs.status == "pending" {
						cs.status = "cancelled"
						cs.outcome = Failed("aborted: parent failed")
						stack = append(stack, child)
					}
				}
			}
		}
		inflight--
	}

	pendingCount := func() int {
		stateMu.Lock()
		defer stateMu.Unlock()
		c := 0
		for _, s := range state {
			if s.status == "pending" || s.status == "running" {
				c++
			}
		}
		return c
	}

	dispatch := func(rid string) {
		js := state[rid].spec
		outcome := o.Runner.Run(ctx, &JobContext{JobID: uuid.New(), JobSpec: js})
		resultsCh <- JobResult{JobSpecRID: rid, Outcome: outcome}
	}

	// Initial dispatch up to parallelism.
	for i := 0; i < parallelism; i++ {
		if rid := nextReady(); rid != "" {
			go dispatch(rid)
		}
	}
	for pendingCount() > 0 {
		r := <-resultsCh
		finish(r)
		if rid := nextReady(); rid != "" {
			go dispatch(rid)
		}
	}

	out := make([]JobResult, 0, len(jobs))
	for _, rid := range order {
		s := state[rid]
		out = append(out, JobResult{JobSpecRID: rid, Outcome: s.outcome})
	}
	return out
}
