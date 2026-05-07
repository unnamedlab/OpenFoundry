package postgres

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/resolver"
	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

func newMockRepo(t *testing.T) (pgxmock.PgxPoolIface, *Repository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewRepository(mock)
}

func TestRepositoryOpenListGetBuild(t *testing.T) {
	mock, repo := newMockRepo(t)
	ctx := context.Background()
	buildID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectExec("INSERT INTO builds").
		WithArgs(buildID, "ri.pipeline.1", "master", pgxmock.AnyArg(), "MANUAL", false, "user-1", "DEPENDENT_ONLY").
		WillReturnResult(pgconn.NewCommandTag("INSERT 0 1"))
	require.NoError(t, repo.OpenBuild(ctx, resolver.ResolveBuildArgs{PipelineRID: "ri.pipeline.1", BuildBranch: "master", RequestedBy: "user-1"}, buildID))

	buildRows := pgxmock.NewRows([]string{"id", "rid", "pipeline_rid", "build_branch", "job_spec_fallback", "state", "trigger_kind", "force_build", "abort_policy", "queued_at", "started_at", "finished_at", "error_message", "requested_by", "created_at"}).
		AddRow(buildID, "ri.foundry.main.build."+buildID.String(), "ri.pipeline.1", "master", []string{}, string(models.BuildResolution), "MANUAL", false, string(models.AbortDependentOnly), nil, nil, nil, nil, "user-1", now)
	mock.ExpectQuery("SELECT id, rid, pipeline_rid").WithArgs("ri.pipeline.1", "", "", pgxmock.AnyArg(), pgxmock.AnyArg(), int64(25)).WillReturnRows(buildRows)
	limit := int64(25)
	items, err := repo.ListBuilds(ctx, models.ListBuildsQuery{PipelineRID: "ri.pipeline.1", Limit: &limit})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, buildID, items[0].ID)

	getRows := pgxmock.NewRows([]string{"id", "rid", "pipeline_rid", "build_branch", "job_spec_fallback", "state", "trigger_kind", "force_build", "abort_policy", "queued_at", "started_at", "finished_at", "error_message", "requested_by", "created_at"}).
		AddRow(buildID, "ri.foundry.main.build."+buildID.String(), "ri.pipeline.1", "master", []string{}, string(models.BuildResolution), "MANUAL", false, string(models.AbortDependentOnly), nil, nil, nil, nil, "user-1", now)
	mock.ExpectQuery("FROM builds WHERE").WithArgs(buildID.String()).WillReturnRows(getRows)
	jobRows := pgxmock.NewRows([]string{"id", "rid", "build_id", "job_spec_rid", "state", "output_transaction_rids", "state_changed_at", "attempt", "stale_skipped", "failure_reason", "output_content_hash", "created_at"})
	mock.ExpectQuery("FROM jobs WHERE build_id").WithArgs(buildID).WillReturnRows(jobRows)
	env, err := repo.GetBuild(ctx, buildID.String())
	require.NoError(t, err)
	require.NotNil(t, env)
	require.Equal(t, buildID, env.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryLookupJobSpecFallback(t *testing.T) {
	mock, repo := newMockRepo(t)
	ctx := context.Background()
	mock.ExpectQuery("FROM pipeline_job_specs").WithArgs("pipe", "out", "feature").WillReturnRows(pgxmock.NewRows([]string{"rid", "pipeline_rid", "branch_name", "inputs", "output_dataset_rid", "job_spec_json", "content_hash"}))
	body := []byte(`{"logic_kind":"TRANSFORM","logic_payload":{"sql":"select 1"},"output_dataset_rids":["out"]}`)
	inputs := []byte(`[{"dataset_rid":"in","fallback_chain":["master"]}]`)
	rows := pgxmock.NewRows([]string{"rid", "pipeline_rid", "branch_name", "inputs", "output_dataset_rid", "job_spec_json", "content_hash"}).AddRow("spec-1", "pipe", "master", inputs, "out", body, "hash")
	mock.ExpectQuery("FROM pipeline_job_specs").WithArgs("pipe", "out", "master").WillReturnRows(rows)
	spec, err := repo.Lookup(ctx, "pipe", "out", "feature", []string{"master"})
	require.NoError(t, err)
	require.NotNil(t, spec)
	require.Equal(t, "TRANSFORM", spec.LogicKind)
	require.Equal(t, []string{"out"}, spec.OutputDatasetRIDs)
	require.Len(t, spec.Inputs, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryPipelineRunsAbortAndLogs(t *testing.T) {
	mock, repo := newMockRepo(t)
	ctx := context.Background()
	pipelineID := uuid.New()
	runID := uuid.New()
	jobID := uuid.New()
	now := time.Now().UTC()

	pipelineRows := pgxmock.NewRows([]string{"id", "name", "description", "owner_id", "dag", "status", "schedule_config", "retry_policy", "next_run_at", "created_at", "updated_at"}).
		AddRow(pipelineID, "p", "", uuid.New(), []byte(`[]`), "active", []byte(`{}`), []byte(`{"max_attempts":1}`), nil, now, now)
	mock.ExpectQuery("FROM pipelines WHERE id").WithArgs(pipelineID).WillReturnRows(pipelineRows)
	p, err := repo.LoadPipeline(ctx, pipelineID)
	require.NoError(t, err)
	require.NotNil(t, p)

	runRows := pgxmock.NewRows([]string{"id", "pipeline_id", "status", "trigger_type", "started_by", "attempt_number", "started_from_node_id", "retry_of_run_id", "execution_context", "node_results", "error_message", "started_at", "finished_at"}).
		AddRow(runID, pipelineID, "running", "manual", nil, int32(1), nil, nil, []byte(`{}`), nil, nil, now, nil)
	mock.ExpectQuery("INSERT INTO pipeline_runs").WithArgs(pgxmock.AnyArg(), pipelineID, pgxmock.AnyArg(), "manual", int32(1), pgxmock.AnyArg(), pgxmock.AnyArg(), json.RawMessage(`{}`)).WillReturnRows(runRows)
	run, err := repo.OpenPipelineRun(ctx, p, models.TriggerPipelineRequest{Context: json.RawMessage(`{}`)}, nil, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, runID, run.ID)
	mock.ExpectExec("UPDATE pipeline_runs SET status").WithArgs(runID, "completed", json.RawMessage(`{"n":"COMPLETED"}`), pgxmock.AnyArg()).WillReturnResult(pgconn.NewCommandTag("UPDATE 1"))
	require.NoError(t, repo.FinishPipelineRun(ctx, runID, "completed", json.RawMessage(`{"n":"COMPLETED"}`), nil))

	mock.ExpectExec("UPDATE builds SET state='BUILD_ABORTING'").WithArgs(runID, "user abort").WillReturnResult(pgconn.NewCommandTag("UPDATE 1"))
	require.NoError(t, repo.MarkBuildAborting(ctx, runID, "user abort"))
	mock.ExpectExec("UPDATE jobs SET state").WithArgs(jobID, string(models.JobAborted), "abort", string(models.JobWaiting)).WillReturnResult(pgconn.NewCommandTag("UPDATE 1"))
	mock.ExpectExec("INSERT INTO job_state_transitions").WithArgs(jobID, string(models.JobWaiting), string(models.JobAborted), "abort").WillReturnResult(pgconn.NewCommandTag("INSERT 0 1"))
	require.NoError(t, repo.TransitionJob(ctx, jobID, models.JobWaiting, models.JobAborted, "abort"))

	logRows := pgxmock.NewRows([]string{"sequence", "job_rid", "ts", "level", "message", "params"}).AddRow(int64(7), jobID.String(), now, string(livellogs.LogInfo), "hello", []byte(`{"x":1}`))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT sequence, $1::text, ts, level, message, params FROM job_logs")).WithArgs(jobID.String(), jobID, int64(0), pgxmock.AnyArg(), pgxmock.AnyArg(), []string{}, int64(10)).WillReturnRows(logRows)
	limit := int64(10)
	logs, err := repo.History(ctx, jobID.String(), livellogs.Query{Limit: limit})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, int64(7), logs[0].Sequence)
	require.NoError(t, mock.ExpectationsWereMet())
}
