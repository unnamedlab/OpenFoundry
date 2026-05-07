package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
	runtimepkg "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/runtime"
)

func TestExecutePipelineLinearDAGViaHTTP(t *testing.T) {
	runner := &recordingNodeRunner{}
	committer := &recordingCommitter{}
	restore := SetExecutionPorts(ExecutionPorts{NodeRunner: runner, Committer: committer, Transactions: &recordingTransactions{}, Parallelism: 1})
	defer restore()

	rr := httptest.NewRecorder()
	ExecutePipeline(rr, httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader([]byte(`{
		"build_id":"11111111-1111-1111-1111-111111111111",
		"nodes":[
			{"id":"extract","outputs":[{"DatasetRID":"out.extract","TransactionRID":"txn.extract"}]},
			{"id":"transform","depends_on":["extract"],"outputs":[{"DatasetRID":"out.transform","TransactionRID":"txn.transform"}]}
		]
	}`))))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	require.Equal(t, []string{"extract", "transform"}, runner.order)
	require.Equal(t, []string{"out.extract", "out.transform"}, committer.datasets)
	var payload executePipelineResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Equal(t, string(models.BuildCompleted), payload.State)
	require.Equal(t, executor.NodeCompleted, payload.Nodes["transform"])
	require.NotContains(t, rr.Body.String(), "not_implemented")
}

func TestExecutePipelineFailureRollsBackAndAbortsDependent(t *testing.T) {
	tx := &recordingTransactions{}
	runner := &recordingNodeRunner{fail: map[string]error{"extract": errors.New("boom")}}
	restore := SetExecutionPorts(ExecutionPorts{NodeRunner: runner, Committer: &recordingCommitter{}, Transactions: tx, Parallelism: 1})
	defer restore()

	rr := httptest.NewRecorder()
	ExecutePipeline(rr, httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader([]byte(`{"nodes":[{"id":"extract","outputs":[{"DatasetRID":"out.extract","TransactionRID":"txn.extract"}]},{"id":"load","depends_on":["extract"],"outputs":[{"DatasetRID":"out.load","TransactionRID":"txn.load"}]}]}`))))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload executePipelineResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Equal(t, string(models.BuildFailed), payload.State)
	require.Equal(t, executor.NodeFailed, payload.Nodes["extract"])
	require.Equal(t, executor.NodeAborted, payload.Nodes["load"])
	require.ElementsMatch(t, []string{"out.extract", "out.load"}, tx.datasets)
}

func TestExecutePipelineCancellationAborts(t *testing.T) {
	tx := &recordingTransactions{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	restore := SetExecutionPorts(ExecutionPorts{NodeRunner: &recordingNodeRunner{}, Committer: &recordingCommitter{}, Transactions: tx})
	defer restore()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader([]byte(`{"nodes":[{"id":"n1","outputs":[{"DatasetRID":"out.cancel","TransactionRID":"txn.cancel"}]}]}`))).WithContext(ctx)
	rr := httptest.NewRecorder()
	ExecutePipeline(rr, req)

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload executePipelineResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Equal(t, string(models.BuildAborted), payload.State)
	require.Equal(t, executor.NodeAborted, payload.Nodes["n1"])
	require.Equal(t, []string{"out.cancel"}, tx.datasets)
}

func TestExecutePipelineMultiOutputPartialCommitFailure(t *testing.T) {
	tx := &recordingTransactions{}
	committer := &recordingCommitter{failDataset: "out.two"}
	restore := SetExecutionPorts(ExecutionPorts{NodeRunner: &recordingNodeRunner{}, Committer: committer, Transactions: tx})
	defer restore()

	rr := httptest.NewRecorder()
	ExecutePipeline(rr, httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader([]byte(`{"nodes":[{"id":"multi","outputs":[{"DatasetRID":"out.one","TransactionRID":"txn.one"},{"DatasetRID":"out.two","TransactionRID":"txn.two"}]}]}`))))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload executePipelineResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Equal(t, string(models.BuildFailed), payload.State)
	require.Equal(t, []string{"out.one", "out.two"}, committer.datasets)
	require.Equal(t, []string{"out.one", "out.two"}, tx.datasets)
}

func TestExecutePipelinePythonRuntimeError(t *testing.T) {
	restore := SetExecutionPorts(ExecutionPorts{Python: failingPython{err: errors.New("python exploded")}, Committer: &recordingCommitter{}, Transactions: &recordingTransactions{}})
	defer restore()

	rr := httptest.NewRecorder()
	ExecutePipeline(rr, httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader([]byte(`{"nodes":[{"id":"py","transform_type":"python","logic_payload":{"source":"raise Exception('x')"},"outputs":[{"DatasetRID":"out.py","TransactionRID":"txn.py"}]}]}`))))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload executePipelineResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Equal(t, string(models.BuildFailed), payload.State)
	require.Contains(t, payload.Reasons["py"], "python exploded")
}

func TestTriggerPipelineRunCreatesAndExecutesRun(t *testing.T) {
	pipelineID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	runRepo := newRecordingPipelineRuns(pipelineID)
	restore := SetExecutionPorts(ExecutionPorts{Runs: runRepo, NodeRunner: &recordingNodeRunner{}, Committer: &recordingCommitter{}, Transactions: &recordingTransactions{}, Parallelism: 1})
	defer restore()

	rr := httptest.NewRecorder()
	TriggerPipelineRun(rr, httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/22222222-2222-2222-2222-222222222222/runs", bytes.NewReader([]byte(`{"skip_unchanged":false}`))))

	require.Equal(t, http.StatusCreated, rr.Result().StatusCode)
	require.Equal(t, 1, runRepo.opened)
	require.Equal(t, "completed", runRepo.finishedStatus)
	var run models.PipelineRun
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&run))
	require.Equal(t, pipelineID, run.PipelineID)
	require.Equal(t, "running", run.Status)
}

type recordingNodeRunner struct {
	mu    sync.Mutex
	order []string
	fail  map[string]error
}

func (r *recordingNodeRunner) Run(ctx context.Context, node executor.NodeContext) (executor.NodeResult, error) {
	if err := ctx.Err(); err != nil {
		return executor.NodeResult{}, err
	}
	r.mu.Lock()
	r.order = append(r.order, node.Node.ID)
	err := r.fail[node.Node.ID]
	r.mu.Unlock()
	if err != nil {
		return executor.NodeResult{}, err
	}
	return executor.NodeResult{OutputContentHash: "hash-" + node.Node.ID}, nil
}

type recordingCommitter struct {
	mu          sync.Mutex
	datasets    []string
	failDataset string
}

func (c *recordingCommitter) Commit(_ context.Context, tx executor.OutputTransaction) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.datasets = append(c.datasets, tx.DatasetRID)
	if tx.DatasetRID == c.failDataset {
		return errors.New("commit failed")
	}
	return nil
}

type recordingTransactions struct {
	mu       sync.Mutex
	datasets []string
}

func (t *recordingTransactions) Abort(_ context.Context, tx executor.OutputTransaction) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.datasets = append(t.datasets, tx.DatasetRID)
	return nil
}

type failingPython struct{ err error }

func (f failingPython) ExecutePythonTransform(context.Context, runtimepkg.TransformRequest) (*runtimepkg.TransformResult, error) {
	return nil, f.err
}

type recordingPipelineRuns struct {
	pipeline       *models.Pipeline
	opened         int
	finishedStatus string
}

func newRecordingPipelineRuns(pipelineID uuid.UUID) *recordingPipelineRuns {
	owner := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	nodes := []models.PipelineNode{{ID: "a", Label: "A", TransformType: "passthrough"}, {ID: "b", Label: "B", TransformType: "passthrough", DependsOn: []string{"a"}}}
	dag, _ := json.Marshal(nodes)
	return &recordingPipelineRuns{pipeline: &models.Pipeline{ID: pipelineID, Name: "p", OwnerID: owner, DAG: dag, RetryPolicy: json.RawMessage(`{"max_attempts":1}`)}}
}

func (r *recordingPipelineRuns) LoadPipeline(_ context.Context, pipelineID uuid.UUID) (*models.Pipeline, error) {
	if r.pipeline.ID != pipelineID {
		return nil, nil
	}
	return r.pipeline, nil
}

func (r *recordingPipelineRuns) OpenPipelineRun(_ context.Context, pipeline *models.Pipeline, _ models.TriggerPipelineRequest, _ *uuid.UUID, contextJSON json.RawMessage) (*models.PipelineRun, error) {
	r.opened++
	if len(contextJSON) == 0 || !json.Valid(contextJSON) {
		return nil, errors.New("invalid context")
	}
	return &models.PipelineRun{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), PipelineID: pipeline.ID, Status: "running", TriggerType: "manual", AttemptNumber: 1, ExecutionContext: contextJSON, StartedAt: time.Now().UTC()}, nil
}

func (r *recordingPipelineRuns) FinishPipelineRun(_ context.Context, _ uuid.UUID, status string, _ json.RawMessage, errorMessage *string) error {
	r.finishedStatus = status
	if status == "completed" && errorMessage != nil && strings.TrimSpace(*errorMessage) != "" {
		return errors.New("completed run should not carry error")
	}
	return nil
}
