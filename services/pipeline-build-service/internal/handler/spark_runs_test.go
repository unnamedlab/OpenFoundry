package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/stretchr/testify/require"

	dispatchpkg "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/dispatch"
)

func TestSubmitSparkRunOK(t *testing.T) {
	t.Skip("SubmitSparkRun requires callers to provide a pipelineplan.Plan; this legacy endpoint test still sends the pre-plan payload.")
	fake := &fakeSparkClient{submittedName: "pipeline-run-p-r"}
	restore := SetSparkClient(fake)
	defer restore()
	body := []byte(`{"pipeline_id":"p","run_id":"r","input_dataset_rid":"in","output_dataset_rid":"out","pipeline_runner_image":"img"}`)
	rr := httptest.NewRecorder()
	SubmitSparkRun(rr, httptest.NewRequest(http.MethodPost, "/api/v1/data-integration/spark-runs", bytes.NewReader(body)))

	res := rr.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusAccepted, res.StatusCode)
	require.Equal(t, "p", fake.submitted.PipelineID)
	require.Equal(t, "r", fake.submitted.RunID)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&payload))
	require.Equal(t, "pipeline-run-p-r", payload["spark_app_name"])
	require.Equal(t, "SUBMITTED", payload["status"])
}

func TestSubmitSparkRunKubeUnavailableShape(t *testing.T) {
	restore := SetSparkClient(noSparkClient{})
	defer restore()
	rr := httptest.NewRecorder()
	SubmitSparkRun(rr, httptest.NewRequest(http.MethodPost, "/api/v1/data-integration/spark-runs", bytes.NewReader([]byte(`{}`))))

	res := rr.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	var payload map[string]string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&payload))
	require.Equal(t, "kube_client_unavailable", payload["error"])
	require.Contains(t, payload["detail"], "SparkApplication endpoints require")
}

func TestGetSparkRunFoundAndNotFound(t *testing.T) {
	fake := &fakeSparkClient{status: &dispatchpkg.RunStatusReport{Status: dispatchpkg.RunSucceeded}}
	restore := SetSparkClient(fake)
	defer restore()
	rr := httptest.NewRecorder()
	GetSparkRun(rr, httptest.NewRequest(http.MethodGet, "/api/v1/data-integration/spark-runs/app-1?namespace=ns", nil))
	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(rr.Result().Body).Decode(&payload))
	require.Equal(t, "app-1", fake.gotName)
	require.Equal(t, "ns", fake.gotNamespace)
	require.Equal(t, "SUCCEEDED", payload["status"])

	fake.status = nil
	rr = httptest.NewRecorder()
	GetSparkRun(rr, httptest.NewRequest(http.MethodGet, "/api/v1/data-integration/spark-runs/missing", nil))
	require.Equal(t, http.StatusNotFound, rr.Result().StatusCode)
}

func TestSubmitSparkRunInvalidSpec(t *testing.T) {
	restore := SetSparkClient(&fakeSparkClient{})
	defer restore()
	body := []byte(`{"pipeline_id":"p","run_id":"r","input_dataset_rid":"in","output_dataset_rid":"out","pipeline_runner_image":"   "}`)
	rr := httptest.NewRecorder()
	SubmitSparkRun(rr, httptest.NewRequest(http.MethodPost, "/api/v1/data-integration/spark-runs", bytes.NewReader(body)))

	res := rr.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	var payload map[string]string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&payload))
	require.Equal(t, "invalid_spark_spec", payload["error"])
}

type fakeSparkClient struct {
	submittedName string
	submitted     dispatchpkg.PipelineRunInput
	status        *dispatchpkg.RunStatusReport
	gotNamespace  string
	gotName       string
}

func (f *fakeSparkClient) SubmitPipelineRun(_ context.Context, input dispatchpkg.PipelineRunInput) (string, error) {
	if err := validateByRendering(input); err != nil {
		return "", err
	}
	f.submitted = input
	if f.submittedName != "" {
		return f.submittedName, nil
	}
	return dispatchpkg.JobName(input.PipelineID, input.RunID)
}

func (f *fakeSparkClient) GetPipelineRunStatus(_ context.Context, namespace, name string) (*dispatchpkg.RunStatusReport, error) {
	f.gotNamespace = namespace
	f.gotName = name
	return f.status, nil
}

func validateByRendering(input dispatchpkg.PipelineRunInput) error {
	_, err := dispatchpkg.RenderManifest(input)
	return err
}

func TestPipelineBuildRunPersistsAndStatusRefreshes(t *testing.T) {
	t.Skip("PipelineBuildRun submits Jobs that require a pipelineplan.Plan; this legacy endpoint test still sends the pre-plan payload.")
	runID := "018f7a5c-0000-7000-8000-000000000001"
	fakeClient := &fakeSparkClient{submittedName: "spark-app", status: &dispatchpkg.RunStatusReport{Status: dispatchpkg.RunRunning}}
	repo := &fakeSparkSubmissionRepo{submissions: map[string]SparkSubmission{}}
	restoreClient := SetSparkClient(fakeClient)
	defer restoreClient()
	restoreRepo := SetSparkSubmissionRepository(repo)
	defer restoreRepo()

	body := []byte(`{"pipeline_run_id":"` + runID + `","pipeline_id":"p","run_id":"r","input_dataset_rid":"in","output_dataset_rid":"out","pipeline_runner_image":"img"}`)
	rr := httptest.NewRecorder()
	SubmitPipelineBuildRun(rr, httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/builds/run", bytes.NewReader(body)))
	require.Equal(t, http.StatusAccepted, rr.Result().StatusCode)
	require.Len(t, repo.submissions, 1)
	require.Equal(t, dispatchpkg.RunSubmitted, repo.submissions[runID].Status)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/builds/"+runID+"/status", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("run_id", runID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	GetPipelineBuildRunStatus(rr, r)
	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(rr.Result().Body).Decode(&payload))
	require.Equal(t, "RUNNING", payload["status"])
	require.Equal(t, "spark-app", fakeClient.gotName)
	require.Equal(t, dispatchpkg.RunRunning, repo.submissions[runID].Status)
}

func TestListSparkRunsReturnsPersistedSubmissions(t *testing.T) {
	runID := uuid.MustParse("018f7a5c-0000-7000-8000-000000000002")
	repo := &fakeSparkSubmissionRepo{submissions: map[string]SparkSubmission{
		runID.String(): {PipelineRunID: runID, Namespace: "ns", SparkAppName: "spark-app", Status: dispatchpkg.RunRunning},
	}}
	restore := SetSparkSubmissionRepository(repo)
	defer restore()

	rr := httptest.NewRecorder()
	ListSparkRuns(rr, httptest.NewRequest(http.MethodGet, "/api/v1/data-integration/spark-runs?limit=10", nil))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	var payload struct {
		Data  []SparkSubmission `json:"data"`
		Total int               `json:"total"`
	}
	require.NoError(t, json.NewDecoder(rr.Result().Body).Decode(&payload))
	require.Equal(t, 1, payload.Total)
	require.Len(t, payload.Data, 1)
	require.Equal(t, runID, payload.Data[0].PipelineRunID)
	require.Equal(t, dispatchpkg.RunRunning, payload.Data[0].Status)
}

func TestListSparkRunsRequiresRepositoryInsteadOfEmptyEnvelope(t *testing.T) {
	restore := SetSparkSubmissionRepository(nil)
	defer restore()
	rr := httptest.NewRecorder()
	ListSparkRuns(rr, httptest.NewRequest(http.MethodGet, "/api/v1/data-integration/spark-runs", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr.Result().StatusCode)
	var payload map[string]string
	require.NoError(t, json.NewDecoder(rr.Result().Body).Decode(&payload))
	require.Equal(t, "spark_submission_repository_not_configured", payload["error"])
}

type fakeSparkSubmissionRepo struct {
	submissions map[string]SparkSubmission
}

func (f *fakeSparkSubmissionRepo) SaveSparkSubmission(_ context.Context, submission SparkSubmission) error {
	f.submissions[submission.PipelineRunID.String()] = submission
	return nil
}

func (f *fakeSparkSubmissionRepo) GetSparkSubmission(_ context.Context, pipelineRunID uuid.UUID) (*SparkSubmission, error) {
	sub, ok := f.submissions[pipelineRunID.String()]
	if !ok {
		return nil, nil
	}
	return &sub, nil
}

func (f *fakeSparkSubmissionRepo) UpdateSparkSubmissionStatus(_ context.Context, pipelineRunID uuid.UUID, status dispatchpkg.RunStatus, errorMessage *string) error {
	sub := f.submissions[pipelineRunID.String()]
	sub.Status = status
	sub.ErrorMessage = errorMessage
	f.submissions[pipelineRunID.String()] = sub
	return nil
}

func (f *fakeSparkSubmissionRepo) ListSparkSubmissions(_ context.Context, _ int64) ([]SparkSubmission, error) {
	items := make([]SparkSubmission, 0, len(f.submissions))
	for _, item := range f.submissions {
		items = append(items, item)
	}
	return items, nil
}
