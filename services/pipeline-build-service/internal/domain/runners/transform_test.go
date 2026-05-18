// Tests for the TRANSFORM runner's python-sidecar integration. The
// fake Manager below implements PythonRuntime so the test asserts
// what the runner forwards to the sidecar (source, config_json,
// input/output dataset ids, timeout) without spawning a subprocess.
package runners

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	pythonsidecar "github.com/openfoundry/openfoundry-go/libs/python-sidecar"
)

type fakeSidecarManager struct {
	result *pythonsidecar.PipelineTransformResult
	err    error

	seenSource   string
	seenConfig   []byte
	seenPrepared []byte
	seenInputIDs []string
	seenOutputID string
	seenTimeout  uint32
	calls        int
}

func (f *fakeSidecarManager) ExecutePipeline(
	_ context.Context,
	source string,
	configJSON []byte,
	preparedInputsJSON []byte,
	inputDatasetIDs []string,
	outputDatasetID string,
	timeoutSeconds uint32,
) (*pythonsidecar.PipelineTransformResult, error) {
	f.calls++
	f.seenSource = source
	f.seenConfig = append([]byte(nil), configJSON...)
	f.seenPrepared = append([]byte(nil), preparedInputsJSON...)
	f.seenInputIDs = append([]string(nil), inputDatasetIDs...)
	f.seenOutputID = outputDatasetID
	f.seenTimeout = timeoutSeconds
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestTransformJobRunnerInvokesSidecarWithConfig(t *testing.T) {
	t.Parallel()
	fake := &fakeSidecarManager{result: &pythonsidecar.PipelineTransformResult{
		RowsAffected:    7,
		RowsAffectedSet: true,
		OutputJSON:      []byte(`{"result":"ok"}`),
		Stdout:          "hi\n",
	}}
	runner := NewTransformJobRunner(fake)

	cfg := json.RawMessage(`{"source":"print('hi')","sql":"select 1"}`)
	jc := &JobContext{
		BuildID: uuid.New(),
		JobID:   uuid.New(),
		JobSpec: JobSpec{
			JobSpecRID:        "job-xform",
			LogicKind:         LogicKindTransform,
			OutputDatasetRIDs: []string{"ri.dataset.out"},
			Config:            cfg,
		},
		ResolvedInputs: []ResolvedInputView{
			{JobSpecRID: "job-xform", DatasetRID: "ri.dataset.in-a", View: "primary"},
			{JobSpecRID: "job-xform", DatasetRID: "ri.dataset.in-b", View: "primary"},
		},
	}

	out := runner.Run(context.Background(), jc)
	if out.Kind != JobOutcomeCompleted {
		t.Fatalf("expected JobOutcomeCompleted, got %+v", out)
	}
	if out.OutputContentHash == "" {
		t.Fatalf("expected non-empty content hash from sidecar metrics")
	}

	if fake.calls != 1 {
		t.Fatalf("expected sidecar invoked once, got %d", fake.calls)
	}
	if fake.seenSource != "print('hi')" {
		t.Fatalf("expected source extracted from config, got %q", fake.seenSource)
	}
	if string(fake.seenConfig) != string(cfg) {
		t.Fatalf("config drift: want %s, got %s", cfg, fake.seenConfig)
	}
	if string(fake.seenPrepared) != "[]" {
		t.Fatalf("expected empty prepared inputs array, got %s", fake.seenPrepared)
	}
	if len(fake.seenInputIDs) != 2 || fake.seenInputIDs[0] != "ri.dataset.in-a" || fake.seenInputIDs[1] != "ri.dataset.in-b" {
		t.Fatalf("input dataset ids drift: %v", fake.seenInputIDs)
	}
	if fake.seenOutputID != "ri.dataset.out" {
		t.Fatalf("output dataset id drift: %q", fake.seenOutputID)
	}
	if fake.seenTimeout != transformSidecarTimeoutSeconds {
		t.Fatalf("timeout drift: want %d, got %d", transformSidecarTimeoutSeconds, fake.seenTimeout)
	}
}

func TestTransformJobRunnerUsesDefaultSourceWhenConfigOmitsIt(t *testing.T) {
	t.Parallel()
	fake := &fakeSidecarManager{result: &pythonsidecar.PipelineTransformResult{
		OutputJSON: []byte(`{"result":null}`),
	}}
	runner := NewTransformJobRunner(fake)
	out := runner.Run(context.Background(), &JobContext{JobSpec: JobSpec{
		LogicKind:         LogicKindTransform,
		OutputDatasetRIDs: []string{"ri.dataset.out"},
		Config:            json.RawMessage(`{"sql":"select 1"}`),
	}})
	if out.Kind != JobOutcomeCompleted {
		t.Fatalf("expected completion, got %+v", out)
	}
	if !strings.Contains(fake.seenSource, "def transform(") {
		t.Fatalf("expected default echo source, got %q", fake.seenSource)
	}
}

func TestTransformJobRunnerSidecarErrorBecomesFailedOutcome(t *testing.T) {
	t.Parallel()
	fake := &fakeSidecarManager{err: errors.New("pipeline transform error: ImportError: boom")}
	runner := NewTransformJobRunner(fake)
	out := runner.Run(context.Background(), &JobContext{JobSpec: JobSpec{
		LogicKind:         LogicKindTransform,
		OutputDatasetRIDs: []string{"ri.dataset.out"},
		Config:            json.RawMessage(`{"source":"raise Exception('boom')"}`),
	}})
	if out.Kind != JobOutcomeFailed {
		t.Fatalf("expected JobOutcomeFailed, got %+v", out)
	}
	if !strings.Contains(out.Reason, "ImportError: boom") {
		t.Fatalf("expected error surfaced, got %q", out.Reason)
	}
}

func TestTransformJobRunnerStubFlagBypassesSidecar(t *testing.T) {
	t.Setenv("OPENFOUNDRY_ENV", "test")
	t.Setenv(transformStubEnvVar, "true")
	fake := &fakeSidecarManager{}
	runner := NewTransformJobRunner(fake)
	out := runner.Run(context.Background(), &JobContext{JobSpec: JobSpec{
		LogicKind:   LogicKindTransform,
		ContentHash: "spec-hash",
		Config:      json.RawMessage(`{"source":"x"}`),
	}})
	if out.Kind != JobOutcomeCompleted || out.OutputContentHash != "spec-hash" {
		t.Fatalf("stub flag must short-circuit to content hash, got %+v", out)
	}
	if fake.calls != 0 {
		t.Fatalf("stub flag must skip sidecar, calls=%d", fake.calls)
	}
}

func TestTransformJobRunnerWithoutSidecarFailsClosed(t *testing.T) {
	t.Parallel()
	runner := NewTransformJobRunner(nil)
	out := runner.Run(context.Background(), &JobContext{JobSpec: JobSpec{
		LogicKind:   LogicKindTransform,
		ContentHash: "spec-hash",
		Config:      json.RawMessage(`{"sql":"select 1"}`),
	}})
	if out.Kind != JobOutcomeFailed || !strings.Contains(out.Reason, "transform runtime unavailable") {
		t.Fatalf("nil sidecar must fail closed, got %+v", out)
	}
}

func TestTransformJobRunnerOutcomeHashChangesWithRowCount(t *testing.T) {
	t.Parallel()
	make := func(rows int64) JobOutcome {
		fake := &fakeSidecarManager{result: &pythonsidecar.PipelineTransformResult{
			RowsAffected:    rows,
			RowsAffectedSet: true,
			OutputJSON:      []byte(`{"result":"ok"}`),
		}}
		runner := NewTransformJobRunner(fake)
		return runner.Run(context.Background(), &JobContext{JobSpec: JobSpec{
			LogicKind:         LogicKindTransform,
			OutputDatasetRIDs: []string{"ri.dataset.out"},
			Config:            json.RawMessage(`{"source":"print('x')"}`),
		}})
	}
	a := make(0)
	b := make(99)
	if a.OutputContentHash == b.OutputContentHash {
		t.Fatalf("output hash should reflect row count metrics; both = %q", a.OutputContentHash)
	}
}

func TestTransformJobRunnerStubFlagFailsInProduction(t *testing.T) {
	t.Setenv("OPENFOUNDRY_ENV", "production")
	t.Setenv(transformStubEnvVar, "true")
	out := NewTransformJobRunner(nil).Run(context.Background(), &JobContext{JobSpec: JobSpec{LogicKind: LogicKindTransform, ContentHash: "spec-hash"}})
	if out.Kind != JobOutcomeFailed || !strings.Contains(out.Reason, "stub mode is disabled") {
		t.Fatalf("production stub flag must fail, got %+v", out)
	}
}
