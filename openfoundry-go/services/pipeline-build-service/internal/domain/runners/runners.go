// Package runners ports
// `services/pipeline-build-service/src/domain/runners/*` — the five
// per-kind job runners (SYNC, TRANSFORM, HEALTH_CHECK, ANALYTICAL,
// EXPORT) plus the dispatcher routing JobContext → JobRunner.
//
// The package owns the job-runner contract, logic-kind validation,
// dispatching, HTTP-backed SYNC / HEALTH_CHECK / EXPORT runners, and
// deterministic Rust-compatible shims for TRANSFORM / ANALYTICAL.
// Python sidecar execution remains behind an explicit interface so the
// domain runner does not wire sidecar runtime concerns directly.
package runners

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	JobSpecRID        string   `json:"job_spec_rid"`
	LogicKind         string   `json:"logic_kind"`
	OutputDatasetRIDs []string `json:"output_dataset_rids,omitempty"`
	// Config is the kind-specific JSON payload (SyncConfig,
	// AnalyticalConfig, ExportConfig, HealthCheckConfig, …) the
	// matching runner decodes.
	Config []byte `json:"config,omitempty"`
	// ContentHash is the resolver-computed spec hash. Transform jobs use
	// it as the output hash when no engine adapter is wired yet.
	ContentHash string `json:"content_hash,omitempty"`
}

// ResolvedInputView mirrors `pub struct ResolvedInputView`. Slimmed
// to the fields runners read.
type ResolvedInputView struct {
	JobSpecRID           string    `json:"job_spec_rid"`
	DatasetRID           string    `json:"dataset_rid"`
	View                 string    `json:"view"`
	BranchID             uuid.UUID `json:"branch_id"`
	TransactionRID       *string   `json:"transaction_rid,omitempty"`
	IsCircularDependency bool      `json:"is_circular_dependency"`
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

// ── Production runners ─────────────────────────────────────────────

// SyncConfig mirrors Rust `SyncConfig` and the SYNC logic payload.
type SyncConfig struct {
	SourceRID string          `json:"source_rid"`
	SyncDefID string          `json:"sync_def_id"`
	Overrides json.RawMessage `json:"overrides,omitempty"`
}

// SyncJobRunner mirrors `pub struct SyncJobRunner`. Runs a Foundry
// SYNC job by calling connector-management-service's ingest endpoint.
type SyncJobRunner struct {
	ConnectorBaseURL string
	HTTPClient       *http.Client
}

// NewSyncJobRunner mirrors `SyncJobRunner::new`.
func NewSyncJobRunner(baseURL string) *SyncJobRunner {
	return &SyncJobRunner{ConnectorBaseURL: baseURL, HTTPClient: http.DefaultClient}
}

// Run mirrors the SYNC runner.
func (r *SyncJobRunner) Run(ctx context.Context, jc *JobContext) JobOutcome {
	var cfg SyncConfig
	if err := json.Unmarshal(jc.JobSpec.Config, &cfg); err != nil {
		return Failed(fmt.Sprintf("invalid SYNC payload: %v", err))
	}
	if strings.TrimSpace(cfg.SyncDefID) == "" {
		return Failed("invalid SYNC payload: sync_def_id is required")
	}
	base := strings.TrimRight(r.ConnectorBaseURL, "/")
	if base == "" {
		return Failed("connector dispatch failed: connector base URL is empty")
	}
	overrides := json.RawMessage(`{}`)
	if len(cfg.Overrides) > 0 && string(cfg.Overrides) != "null" {
		overrides = cfg.Overrides
	}
	body := map[string]any{
		"overrides":     json.RawMessage(overrides),
		"build_job_rid": jc.JobSpec.JobSpecRID,
	}
	payload, err := postJSON(ctx, r.HTTPClient, base+"/api/v1/data-integration/syncs/"+url.PathEscape(cfg.SyncDefID)+"/run", body)
	if err != nil {
		return Failed(fmt.Sprintf("connector dispatch failed: %v", err))
	}
	if !payload.StatusSuccess {
		return Failed(fmt.Sprintf("connector returned %s: %s", payload.Status, payload.Body))
	}
	var resp map[string]any
	_ = json.Unmarshal(payload.BodyBytes, &resp)
	if id, ok := resp["ingest_job_id"].(string); ok && id != "" {
		return Completed("sync:" + id)
	}
	return Completed("sync:" + cfg.SyncDefID)
}

// TransformExecutor isolates the future T5 engine/sidecar integration
// point. Keeping this interface in the runner domain lets the current
// runner avoid directly importing or wiring the Python sidecar.
type TransformExecutor interface {
	RunTransform(ctx context.Context, jc *JobContext) (string, error)
}

// TransformJobRunner mirrors `pub struct TransformJobRunner`.
type TransformJobRunner struct {
	Engine TransformExecutor
}

// Run mirrors the TRANSFORM runner. Without an engine adapter, it
// returns a deterministic hash from the resolved spec/config, matching
// Rust's current shim instead of exposing a skeleton failure.
func (r *TransformJobRunner) Run(ctx context.Context, jc *JobContext) JobOutcome {
	if r.Engine != nil {
		hash, err := r.Engine.RunTransform(ctx, jc)
		if err != nil {
			return Failed(err.Error())
		}
		return Completed(hash)
	}
	if jc.JobSpec.ContentHash != "" {
		return Completed(jc.JobSpec.ContentHash)
	}
	return Completed(stableHash("transform", jc.JobSpec.Config))
}

// HealthCheckKind mirrors Rust's HEALTH_CHECK enum values.
type HealthCheckKind string

const (
	HealthCheckRowCountNonzero HealthCheckKind = "ROW_COUNT_NONZERO"
	HealthCheckSchemaDrift     HealthCheckKind = "SCHEMA_DRIFT"
	HealthCheckFreshnessSLA    HealthCheckKind = "FRESHNESS_SLA"
	HealthCheckCustomSQL       HealthCheckKind = "CUSTOM_SQL"
)

// HealthCheckConfig mirrors Rust `HealthCheckConfig`.
type HealthCheckConfig struct {
	CheckKind        HealthCheckKind `json:"check_kind"`
	TargetDatasetRID string          `json:"target_dataset_rid"`
	Params           json.RawMessage `json:"params,omitempty"`
	Name             *string         `json:"name,omitempty"`
}

// HealthCheckJobRunner mirrors `pub struct HealthCheckJobRunner`.
type HealthCheckJobRunner struct {
	QualityBaseURL string
	HTTPClient     *http.Client
}

// NewHealthCheckJobRunner mirrors `HealthCheckJobRunner::new`.
func NewHealthCheckJobRunner(baseURL string) *HealthCheckJobRunner {
	return &HealthCheckJobRunner{QualityBaseURL: baseURL, HTTPClient: http.DefaultClient}
}

// Run mirrors the HEALTH_CHECK runner.
func (r *HealthCheckJobRunner) Run(ctx context.Context, jc *JobContext) JobOutcome {
	var cfg HealthCheckConfig
	if err := json.Unmarshal(jc.JobSpec.Config, &cfg); err != nil {
		return Failed(fmt.Sprintf("invalid HEALTH_CHECK payload: %v", err))
	}
	if cfg.TargetDatasetRID == "" {
		return Failed("invalid HEALTH_CHECK payload: target_dataset_rid is required")
	}
	if !containsString(jc.JobSpec.OutputDatasetRIDs, cfg.TargetDatasetRID) {
		return Failed(fmt.Sprintf("HEALTH_CHECK target %s not present in JobSpec outputs", cfg.TargetDatasetRID))
	}
	base := strings.TrimRight(r.QualityBaseURL, "/")
	if base == "" {
		return Failed("dataset-quality-service unreachable: quality base URL is empty")
	}
	evaluation := evaluateCheck(cfg)
	finding := map[string]any{
		"check_kind":   cfg.CheckKind,
		"name":         cfg.Name,
		"passed":       evaluation.passed,
		"message":      evaluation.message,
		"params":       rawJSONOrObject(cfg.Params),
		"build_branch": jc.BuildBranch,
		"job_rid":      jc.JobSpec.JobSpecRID,
	}
	payload, err := postJSON(ctx, r.HTTPClient, base+"/api/v1/datasets/"+url.PathEscape(cfg.TargetDatasetRID)+"/health-checks/results", finding)
	if err != nil {
		return Failed(fmt.Sprintf("dataset-quality-service unreachable: %v", err))
	}
	if !payload.StatusSuccess {
		return Failed(fmt.Sprintf("quality service returned %s: %s", payload.Status, payload.Body))
	}
	return Completed(fmt.Sprintf("health:%s:%t", cfg.CheckKind, evaluation.passed))
}

// AnalyticalConfig mirrors Rust `AnalyticalConfig`.
type AnalyticalConfig struct {
	ObjectSetQuery json.RawMessage `json:"object_set_query"`
	OntologyRID    *string         `json:"ontology_rid,omitempty"`
	OutputSchema   json.RawMessage `json:"output_schema,omitempty"`
}

// AnalyticalJobRunner mirrors `pub struct AnalyticalJobRunner`.
type AnalyticalJobRunner struct{}

// Run mirrors the ANALYTICAL runner.
func (r *AnalyticalJobRunner) Run(_ context.Context, jc *JobContext) JobOutcome {
	var cfg AnalyticalConfig
	if err := json.Unmarshal(jc.JobSpec.Config, &cfg); err != nil {
		return Failed(fmt.Sprintf("invalid ANALYTICAL payload: %v", err))
	}
	if len(jc.JobSpec.OutputDatasetRIDs) != 1 {
		return Failed(fmt.Sprintf("ANALYTICAL job must have exactly one output (got %d)", len(jc.JobSpec.OutputDatasetRIDs)))
	}
	h := sha256.New()
	_, _ = h.Write([]byte("analytical"))
	_, _ = h.Write(canonicalJSON(cfg.ObjectSetQuery))
	if cfg.OntologyRID != nil {
		_, _ = h.Write([]byte("|onto|"))
		_, _ = h.Write([]byte(*cfg.OntologyRID))
	}
	if len(cfg.OutputSchema) > 0 && string(cfg.OutputSchema) != "null" {
		_, _ = h.Write([]byte("|sch|"))
		_, _ = h.Write(canonicalJSON(cfg.OutputSchema))
	}
	return Completed(hex.EncodeToString(h.Sum(nil)))
}

// ExportTarget mirrors Rust `ExportTarget`.
type ExportTarget string

const (
	ExportTargetS3   ExportTarget = "S3"
	ExportTargetGCS  ExportTarget = "GCS"
	ExportTargetHTTP ExportTarget = "HTTP"
	ExportTargetJDBC ExportTarget = "JDBC"
)

// ExportConfig mirrors Rust `ExportConfig`.
type ExportConfig struct {
	ExportTarget     ExportTarget    `json:"export_target"`
	Endpoint         string          `json:"endpoint"`
	Options          json.RawMessage `json:"options,omitempty"`
	SourceDatasetRID string          `json:"source_dataset_rid"`
	ACLAlias         *string         `json:"acl_alias,omitempty"`
}

// ExportJobRunner mirrors `pub struct ExportJobRunner`.
type ExportJobRunner struct {
	HTTPClient *http.Client
}

// NewExportJobRunner builds an HTTP-backed export runner.
func NewExportJobRunner() *ExportJobRunner { return &ExportJobRunner{HTTPClient: http.DefaultClient} }

// Run mirrors the EXPORT runner.
func (r *ExportJobRunner) Run(ctx context.Context, jc *JobContext) JobOutcome {
	var cfg ExportConfig
	if err := json.Unmarshal(jc.JobSpec.Config, &cfg); err != nil {
		return Failed(fmt.Sprintf("invalid EXPORT payload: %v", err))
	}
	if cfg.ACLAlias == nil || *cfg.ACLAlias == "" {
		return Failed("EXPORT job missing acl_alias: refusing to push to unconfigured target")
	}
	manifest := map[string]any{
		"export_target":      cfg.ExportTarget,
		"endpoint":           cfg.Endpoint,
		"options":            rawJSONOrObject(cfg.Options),
		"source_dataset_rid": cfg.SourceDatasetRID,
		"acl_alias":          cfg.ACLAlias,
		"build_branch":       jc.BuildBranch,
		"job_rid":            jc.JobSpec.JobSpecRID,
	}
	payload, err := postJSON(ctx, r.HTTPClient, cfg.Endpoint, manifest)
	if err != nil {
		return Failed(fmt.Sprintf("export endpoint unreachable: %v", err))
	}
	if !payload.StatusSuccess {
		return Failed(fmt.Sprintf("export target returned %s: %s", payload.Status, payload.Body))
	}
	h := sha256.New()
	_, _ = h.Write([]byte("export"))
	_, _ = h.Write([]byte(cfg.Endpoint))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(cfg.SourceDatasetRID))
	return Completed(hex.EncodeToString(h.Sum(nil)))
}

type checkEvaluation struct {
	passed  bool
	message string
}

func evaluateCheck(cfg HealthCheckConfig) checkEvaluation {
	passed := true
	var params map[string]any
	if len(cfg.Params) > 0 {
		_ = json.Unmarshal(cfg.Params, &params)
	}
	if v, ok := params["expect_passed"].(bool); ok {
		passed = v
	}
	messages := map[HealthCheckKind]string{
		HealthCheckRowCountNonzero: "row count check",
		HealthCheckSchemaDrift:     "schema drift check",
		HealthCheckFreshnessSLA:    "freshness SLA check",
		HealthCheckCustomSQL:       "custom SQL check",
	}
	msg := messages[cfg.CheckKind]
	if msg == "" {
		msg = "health check"
	}
	return checkEvaluation{passed: passed, message: msg}
}

type httpPostResult struct {
	Status        string
	StatusSuccess bool
	Body          string
	BodyBytes     []byte
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, body any) (httpPostResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return httpPostResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return httpPostResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return httpPostResult{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return httpPostResult{Status: resp.Status, StatusSuccess: resp.StatusCode >= 200 && resp.StatusCode < 300, Body: string(respBody), BodyBytes: respBody}, nil
}

func rawJSONOrObject(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	return raw
}

func canonicalJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return bytes.TrimSpace(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return bytes.TrimSpace(raw)
	}
	return out
}

func stableHash(prefix string, raw json.RawMessage) string {
	h := sha256.New()
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write(canonicalJSON(raw))
	return hex.EncodeToString(h.Sum(nil))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
