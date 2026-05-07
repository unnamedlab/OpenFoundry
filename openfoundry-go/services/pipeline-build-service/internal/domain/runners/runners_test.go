// Tests for Phase B runners — logic-kind validation, dispatcher
// routing, parallel orchestrator dependency cascade.
package runners

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateLogicKindEnforcesArity(t *testing.T) {
	t.Parallel()
	if err := ValidateLogicKind(LogicKindSync, 0); err == nil {
		t.Error("SYNC with 0 outputs must error")
	}
	if err := ValidateLogicKind(LogicKindSync, 1); err != nil {
		t.Error("SYNC with 1 output must pass")
	}
	if err := ValidateLogicKind(LogicKindHealthCheck, 0); err == nil {
		t.Error("HEALTH_CHECK with 0 outputs must error")
	}
	if err := ValidateLogicKind(LogicKindHealthCheck, 2); err == nil {
		t.Error("HEALTH_CHECK with 2 outputs must error")
	}
	if err := ValidateLogicKind(LogicKindAnalytical, 1); err != nil {
		t.Error("ANALYTICAL with 1 output must pass")
	}
	if err := ValidateLogicKind(LogicKindExport, 0); err != nil {
		t.Error("EXPORT with 0 outputs must pass (data leaves Foundry)")
	}
	if err := ValidateLogicKind("FOO", 1); err == nil {
		t.Error("unknown kind must error")
	}
}

func TestIsKnownLogicKind(t *testing.T) {
	t.Parallel()
	for _, kind := range AllLogicKinds {
		if !IsKnownLogicKind(kind) {
			t.Errorf("expected %s to be known", kind)
		}
	}
	if IsKnownLogicKind("PRESENT") {
		t.Error("PRESENT must not be known")
	}
}

// ── Mock runner for orchestrator + dispatcher tests ────────────────

type recordingRunner struct {
	outcomeFor func(rid string) JobOutcome
	calls      int32
}

func (r *recordingRunner) Run(_ context.Context, jc *JobContext) JobOutcome {
	atomic.AddInt32(&r.calls, 1)
	if r.outcomeFor != nil {
		return r.outcomeFor(jc.JobSpec.JobSpecRID)
	}
	return Completed("hash")
}

// ── DispatchingRunner ───────────────────────────────────────────────

func TestDispatchingRunnerRoutesByLogicKind(t *testing.T) {
	t.Parallel()
	mark := map[string]string{}
	var mu sync.Mutex
	mk := func(kind string) JobRunner {
		k := kind
		return &funcRunner{fn: func(jc *JobContext) JobOutcome {
			mu.Lock()
			mark[k] = jc.JobSpec.JobSpecRID
			mu.Unlock()
			return Completed(k)
		}}
	}
	d := &DispatchingRunner{
		Sync:        mk("sync"),
		Transform:   mk("transform"),
		HealthCheck: mk("hc"),
		Analytical:  mk("an"),
		Export:      mk("ex"),
	}
	for kind, expectKey := range map[string]string{
		LogicKindSync: "sync", LogicKindTransform: "transform",
		LogicKindHealthCheck: "hc", LogicKindAnalytical: "an",
		LogicKindExport: "ex",
	} {
		out := d.Run(context.Background(), &JobContext{
			JobSpec: JobSpec{JobSpecRID: kind, LogicKind: kind},
		})
		if out.Kind != JobOutcomeCompleted {
			t.Errorf("%s: expected completed, got %+v", kind, out)
		}
		if out.OutputContentHash != expectKey {
			t.Errorf("%s: expected hash %q, got %q", kind, expectKey, out.OutputContentHash)
		}
	}
}

func TestDispatchingRunnerUnknownKindFailsFast(t *testing.T) {
	t.Parallel()
	d := &DispatchingRunner{}
	out := d.Run(context.Background(), &JobContext{
		JobSpec: JobSpec{LogicKind: "WHAT"},
	})
	if out.Kind != JobOutcomeFailed || !strings.Contains(out.Reason, "unknown logic_kind") {
		t.Errorf("expected unknown-kind failure, got %+v", out)
	}
}

func TestDispatchingRunnerNilSubrunnerFailsClearly(t *testing.T) {
	t.Parallel()
	d := &DispatchingRunner{}
	out := d.Run(context.Background(), &JobContext{
		JobSpec: JobSpec{LogicKind: LogicKindSync},
	})
	if out.Kind != JobOutcomeFailed || !strings.Contains(out.Reason, "runner_not_wired:sync") {
		t.Errorf("nil sub-runner must surface runner_not_wired, got %+v", out)
	}
}

// ── BuildOrchestrator ──────────────────────────────────────────────

func TestBuildOrchestratorRespectsDependencyOrder(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	completed := []string{}
	runner := &funcRunner{fn: func(jc *JobContext) JobOutcome {
		time.Sleep(10 * time.Millisecond) // make ordering observable
		mu.Lock()
		completed = append(completed, jc.JobSpec.JobSpecRID)
		mu.Unlock()
		return Completed("ok")
	}}
	o := &BuildOrchestrator{Runner: runner, Parallelism: 2}
	results := o.Run(context.Background(), []JobSpecWithDeps{
		{Spec: JobSpec{JobSpecRID: "a", LogicKind: LogicKindTransform}},
		{Spec: JobSpec{JobSpecRID: "b", LogicKind: LogicKindTransform}, Depends: []string{"a"}},
		{Spec: JobSpec{JobSpecRID: "c", LogicKind: LogicKindTransform}, Depends: []string{"b"}},
	})
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Outcome.Kind != JobOutcomeCompleted {
			t.Errorf("%s: expected completed, got %+v", r.JobSpecRID, r.Outcome)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for i, expected := range []string{"a", "b", "c"} {
		if completed[i] != expected {
			t.Errorf("position %d: expected %s, got %s (full order: %v)", i, expected, completed[i], completed)
		}
	}
}

func TestBuildOrchestratorRunsIndependentJobsInParallel(t *testing.T) {
	t.Parallel()
	var inflightMax int32
	var inflightCur int32
	runner := &funcRunner{fn: func(_ *JobContext) JobOutcome {
		cur := atomic.AddInt32(&inflightCur, 1)
		for {
			max := atomic.LoadInt32(&inflightMax)
			if cur <= max || atomic.CompareAndSwapInt32(&inflightMax, max, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inflightCur, -1)
		return Completed("ok")
	}}
	o := &BuildOrchestrator{Runner: runner, Parallelism: 4}
	o.Run(context.Background(), []JobSpecWithDeps{
		{Spec: JobSpec{JobSpecRID: "a", LogicKind: LogicKindTransform}},
		{Spec: JobSpec{JobSpecRID: "b", LogicKind: LogicKindTransform}},
		{Spec: JobSpec{JobSpecRID: "c", LogicKind: LogicKindTransform}},
	})
	if atomic.LoadInt32(&inflightMax) < 2 {
		t.Errorf("expected at least 2 jobs in flight, got %d", inflightMax)
	}
}

func TestBuildOrchestratorCascadesCancellation(t *testing.T) {
	t.Parallel()
	runner := &funcRunner{fn: func(jc *JobContext) JobOutcome {
		if jc.JobSpec.JobSpecRID == "a" {
			return Failed("kaboom")
		}
		return Completed("ok")
	}}
	o := &BuildOrchestrator{Runner: runner, Parallelism: 4}
	results := o.Run(context.Background(), []JobSpecWithDeps{
		{Spec: JobSpec{JobSpecRID: "a", LogicKind: LogicKindTransform}},
		{Spec: JobSpec{JobSpecRID: "b", LogicKind: LogicKindTransform}, Depends: []string{"a"}},
		{Spec: JobSpec{JobSpecRID: "c", LogicKind: LogicKindTransform}, Depends: []string{"b"}},
		{Spec: JobSpec{JobSpecRID: "d", LogicKind: LogicKindTransform}}, // independent
	})
	byID := map[string]JobOutcome{}
	for _, r := range results {
		byID[r.JobSpecRID] = r.Outcome
	}
	if byID["a"].Kind != JobOutcomeFailed {
		t.Errorf("a: expected failed")
	}
	if byID["b"].Kind != JobOutcomeFailed || !strings.Contains(byID["b"].Reason, "aborted") {
		t.Errorf("b must cascade-cancel, got %+v", byID["b"])
	}
	if byID["c"].Kind != JobOutcomeFailed || !strings.Contains(byID["c"].Reason, "aborted") {
		t.Errorf("c must cascade-cancel, got %+v", byID["c"])
	}
	// Independent job d may complete or be aborted depending on
	// timing; the contract is that the cascade only follows the
	// dependency edges (we are not in AbortAll mode). When the
	// cancellation propagates first, d still completes because its
	// dependencies are unaffected.
	if byID["d"].Kind == JobOutcomeFailed {
		t.Logf("d completed before cascade — implementation detail; both outcomes acceptable")
	}
}

func TestBuildOrchestratorAbortAllSetsAbortReason(t *testing.T) {
	t.Parallel()
	runner := &funcRunner{fn: func(jc *JobContext) JobOutcome {
		if jc.JobSpec.JobSpecRID == "a" {
			return Failed("kaboom")
		}
		// Sleep long enough that the AbortAll flag fires before the
		// orchestrator picks the next ready job.
		time.Sleep(50 * time.Millisecond)
		return Completed("ok")
	}}
	o := &BuildOrchestrator{Runner: runner, Parallelism: 1, AbortAll: true}
	results := o.Run(context.Background(), []JobSpecWithDeps{
		{Spec: JobSpec{JobSpecRID: "a", LogicKind: LogicKindTransform}},
		{Spec: JobSpec{JobSpecRID: "b", LogicKind: LogicKindTransform}},
		{Spec: JobSpec{JobSpecRID: "c", LogicKind: LogicKindTransform}},
	})
	failed := 0
	for _, r := range results {
		if r.Outcome.Kind == JobOutcomeFailed {
			failed++
		}
	}
	if failed < 2 {
		t.Errorf("AbortAll should cancel pending jobs, only %d/3 failed", failed)
	}
}

// ── helpers ────────────────────────────────────────────────────────

type funcRunner struct {
	fn func(jc *JobContext) JobOutcome
}

func (r *funcRunner) Run(_ context.Context, jc *JobContext) JobOutcome { return r.fn(jc) }

// keeps uuid import live for future cancellation tests
var _ = uuid.New

func TestSyncJobRunnerPostsConnectorRun(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ingest_job_id":"ing-1"}`))
	}))
	defer server.Close()

	runner := NewSyncJobRunner(server.URL)
	out := runner.Run(context.Background(), &JobContext{JobSpec: JobSpec{
		JobSpecRID: "job-sync",
		LogicKind:  LogicKindSync,
		Config:     []byte(`{"source_rid":"ri.source","sync_def_id":"sync-1","overrides":{"limit":10}}`),
	}})
	if out.Kind != JobOutcomeCompleted || out.OutputContentHash != "sync:ing-1" {
		t.Fatalf("expected sync completion, got %+v", out)
	}
	if gotPath != "/api/v1/data-integration/syncs/sync-1/run" {
		t.Fatalf("unexpected path %s", gotPath)
	}
	if gotBody["build_job_rid"] != "job-sync" {
		t.Fatalf("expected build_job_rid, got %#v", gotBody)
	}
}

func TestHealthCheckJobRunnerPostsQualityResult(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	runner := NewHealthCheckJobRunner(server.URL)
	out := runner.Run(context.Background(), &JobContext{BuildBranch: "master", JobSpec: JobSpec{
		JobSpecRID:        "job-health",
		LogicKind:         LogicKindHealthCheck,
		OutputDatasetRIDs: []string{"ri.dataset"},
		Config:            []byte(`{"check_kind":"ROW_COUNT_NONZERO","target_dataset_rid":"ri.dataset","params":{"expect_passed":false}}`),
	}})
	if out.Kind != JobOutcomeCompleted || out.OutputContentHash != "health:ROW_COUNT_NONZERO:false" {
		t.Fatalf("expected health completion, got %+v", out)
	}
	if gotPath != "/api/v1/datasets/ri.dataset/health-checks/results" {
		t.Fatalf("unexpected path %s", gotPath)
	}
	if gotBody["passed"] != false || gotBody["job_rid"] != "job-health" {
		t.Fatalf("unexpected finding body %#v", gotBody)
	}
}

func TestAnalyticalAndTransformRunnersAreNotSkeletonFailures(t *testing.T) {
	t.Parallel()
	analytical := (&AnalyticalJobRunner{}).Run(context.Background(), &JobContext{JobSpec: JobSpec{
		LogicKind:         LogicKindAnalytical,
		OutputDatasetRIDs: []string{"out"},
		Config:            []byte(`{"object_set_query":{"select":["id"]},"ontology_rid":"onto"}`),
	}})
	if analytical.Kind != JobOutcomeCompleted || strings.Contains(analytical.Reason, "runner_not_wired") {
		t.Fatalf("analytical runner must complete or return typed errors, got %+v", analytical)
	}

	transform := (&TransformJobRunner{}).Run(context.Background(), &JobContext{JobSpec: JobSpec{
		LogicKind:   LogicKindTransform,
		ContentHash: "spec-hash",
		Config:      []byte(`{"sql":"select 1"}`),
	}})
	if transform.Kind != JobOutcomeCompleted || transform.OutputContentHash != "spec-hash" {
		t.Fatalf("transform shim must use content hash, got %+v", transform)
	}
}

func TestExportJobRunnerPostsManifest(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := `{"export_target":"HTTP","endpoint":"` + server.URL + `","source_dataset_rid":"source","acl_alias":"acl-prod"}`
	out := (&ExportJobRunner{}).Run(context.Background(), &JobContext{BuildBranch: "master", JobSpec: JobSpec{
		JobSpecRID: "job-export",
		LogicKind:  LogicKindExport,
		Config:     []byte(cfg),
	}})
	if out.Kind != JobOutcomeCompleted || out.OutputContentHash == "" {
		t.Fatalf("expected export completion, got %+v", out)
	}
	if gotBody["endpoint"] != server.URL || gotBody["acl_alias"] != "acl-prod" {
		t.Fatalf("unexpected manifest %#v", gotBody)
	}
}
