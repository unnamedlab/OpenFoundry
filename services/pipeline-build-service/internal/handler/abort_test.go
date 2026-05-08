package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

func TestAbortBuildNotFound(t *testing.T) {
	repo := newAbortRepo(nil)
	restore := SetExecutionPorts(ExecutionPorts{Plans: repo, Transactions: &abortTxManager{}})
	defer restore()

	rr := httptest.NewRecorder()
	AbortBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds/missing/abort", nil))

	require.Equal(t, http.StatusNotFound, rr.Result().StatusCode)
}

func TestAbortBuildPendingMarksWaitingJobsAborted(t *testing.T) {
	buildID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	jobID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	repo := newAbortRepo(&AbortBuildSnapshot{ID: buildID, RID: "ri.build.pending", State: models.BuildQueued, Jobs: []AbortJobSnapshot{{ID: jobID, State: models.JobWaiting, Outputs: []executor.OutputTransaction{{DatasetRID: "out.pending", TransactionRID: "txn.pending"}}}}})
	tx := &abortTxManager{}
	restore := SetExecutionPorts(ExecutionPorts{Plans: repo, Transactions: tx})
	defer restore()

	rr := httptest.NewRecorder()
	AbortBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds/ri.build.pending/abort", nil))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	require.Equal(t, models.BuildAborted, repo.state)
	require.Equal(t, []models.JobState{models.JobAborted}, repo.transitions[jobID])
	require.Equal(t, []string{"out.pending"}, tx.aborted)
	require.NotContains(t, rr.Body.String(), "not_implemented")
}

func TestAbortBuildRunningCancelsExecutionAndTransitionsJobs(t *testing.T) {
	buildID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	runningJob := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	pendingJob := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	repo := newAbortRepo(&AbortBuildSnapshot{ID: buildID, RID: "ri.build.running", State: models.BuildRunning, Jobs: []AbortJobSnapshot{
		{ID: runningJob, State: models.JobRunning, Outputs: []executor.OutputTransaction{{DatasetRID: "out.running", TransactionRID: "txn.running"}}},
		{ID: pendingJob, State: models.JobRunPending, Outputs: []executor.OutputTransaction{{DatasetRID: "out.pending", TransactionRID: "txn.pending"}}},
	}})
	ctx, cancel := context.WithCancel(context.Background())
	unregister := registerExecutionCancel(buildID, cancel)
	defer unregister()
	tx := &abortTxManager{}
	restore := SetExecutionPorts(ExecutionPorts{Plans: repo, Transactions: tx})
	defer restore()

	rr := httptest.NewRecorder()
	AbortBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds/ri.build.running/abort", nil))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, []models.JobState{models.JobAbortPending, models.JobAborted}, repo.transitions[runningJob])
	require.Equal(t, []models.JobState{models.JobAbortPending, models.JobAborted}, repo.transitions[pendingJob])
	require.ElementsMatch(t, []string{"out.running", "out.pending"}, tx.aborted)
}

func TestAbortBuildIdempotentWhenAlreadyAborting(t *testing.T) {
	buildID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	jobID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	repo := newAbortRepo(&AbortBuildSnapshot{ID: buildID, RID: "ri.build.aborting", State: models.BuildAborting, Jobs: []AbortJobSnapshot{{ID: jobID, State: models.JobAbortPending}}})
	restore := SetExecutionPorts(ExecutionPorts{Plans: repo, Transactions: &abortTxManager{}})
	defer restore()

	rr := httptest.NewRecorder()
	AbortBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds/ri.build.aborting/abort", nil))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	require.Equal(t, 0, repo.markAbortingCalls)
	require.Equal(t, []models.JobState{models.JobAborted}, repo.transitions[jobID])
}

func TestAbortBuildTransactionAbortFailure(t *testing.T) {
	buildID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	repo := newAbortRepo(&AbortBuildSnapshot{ID: buildID, RID: "ri.build.tx", State: models.BuildRunning, Jobs: []AbortJobSnapshot{{ID: uuid.New(), State: models.JobRunning, Outputs: []executor.OutputTransaction{{DatasetRID: "out.bad", TransactionRID: "txn.bad"}}}}})
	restore := SetExecutionPorts(ExecutionPorts{Plans: repo, Transactions: &abortTxManager{fail: map[string]error{"out.bad": errors.New("catalog down")}}})
	defer restore()

	rr := httptest.NewRecorder()
	AbortBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds/ri.build.tx/abort", nil))

	require.Equal(t, http.StatusBadGateway, rr.Result().StatusCode)
	require.Contains(t, rr.Body.String(), "catalog down")
	require.NotEqual(t, models.BuildAborted, repo.state)
}

type abortRepo struct {
	snapshot          *AbortBuildSnapshot
	state             models.BuildState
	transitions       map[uuid.UUID][]models.JobState
	markAbortingCalls int
	markAbortedCalls  int
}

func newAbortRepo(snapshot *AbortBuildSnapshot) *abortRepo {
	repo := &abortRepo{snapshot: snapshot, transitions: map[uuid.UUID][]models.JobState{}}
	if snapshot != nil {
		repo.state = snapshot.State
	}
	return repo
}

func (r *abortRepo) LoadPlan(context.Context, uuid.UUID) (executor.Plan, error) {
	return executor.Plan{}, nil
}

func (r *abortRepo) LoadBuildForAbort(_ context.Context, id string) (*AbortBuildSnapshot, error) {
	if r.snapshot == nil || (id != r.snapshot.RID && id != r.snapshot.ID.String()) {
		return nil, nil
	}
	copy := *r.snapshot
	copy.State = r.state
	return &copy, nil
}

func (r *abortRepo) MarkBuildAborting(_ context.Context, _ uuid.UUID, _ string) error {
	r.markAbortingCalls++
	r.state = models.BuildAborting
	return nil
}

func (r *abortRepo) TransitionJob(_ context.Context, jobID uuid.UUID, _, to models.JobState, _ string) error {
	r.transitions[jobID] = append(r.transitions[jobID], to)
	return nil
}

func (r *abortRepo) MarkBuildAborted(_ context.Context, _ uuid.UUID, _ string) error {
	r.markAbortedCalls++
	r.state = models.BuildAborted
	return nil
}

type abortTxManager struct {
	aborted []string
	fail    map[string]error
}

func (m *abortTxManager) Abort(_ context.Context, tx executor.OutputTransaction) error {
	if err := m.fail[tx.DatasetRID]; err != nil {
		return err
	}
	m.aborted = append(m.aborted, tx.DatasetRID)
	return nil
}
