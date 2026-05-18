package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/repo"
)

func newRouter(store repo.Store) http.Handler {
	r := chi.NewRouter()
	h := &handlers.Jobs{Store: store}
	h.Mount(r)
	return r
}

func TestJobs_CreateAndGet(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)

	body := mustJSON(t, models.CreateJobRequest{
		SourceURI: "s3://docs/a.pdf",
		Pipeline:  "ocr",
	})
	resp := do(t, r, http.MethodPost, "/jobs", body)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var j models.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&j))
	resp.Body.Close()
	require.Equal(t, models.JobStatusQueued, j.Status)

	got := do(t, r, http.MethodGet, "/jobs/"+j.ID.String(), nil)
	require.Equal(t, http.StatusOK, got.StatusCode)
	got.Body.Close()
}

func TestJobs_Create_MissingFieldRejected(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodPost, "/jobs", mustJSON(t, models.CreateJobRequest{Pipeline: "ocr"}))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp.Body.Close()
}

func TestJobs_UpdateRejectsIllegalTransition(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodPost, "/jobs", mustJSON(t, models.CreateJobRequest{
		SourceURI: "s3://x", Pipeline: "ocr",
	}))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var j models.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&j))
	resp.Body.Close()

	// queued -> succeeded is illegal (must go through running first).
	bad := do(t, r, http.MethodPatch, "/jobs/"+j.ID.String(),
		bytes.NewReader([]byte(`{"status":"succeeded"}`)))
	assert.Equal(t, http.StatusPreconditionFailed, bad.StatusCode)
	bad.Body.Close()
}

func TestJobs_UpdateAcceptsLegalTransition(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodPost, "/jobs", mustJSON(t, models.CreateJobRequest{
		SourceURI: "s3://x", Pipeline: "ocr",
	}))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var j models.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&j))
	resp.Body.Close()

	ok := do(t, r, http.MethodPatch, "/jobs/"+j.ID.String(),
		bytes.NewReader([]byte(`{"status":"running"}`)))
	assert.Equal(t, http.StatusOK, ok.StatusCode)
	ok.Body.Close()
}

func TestJobs_AppendEventAndListEvents(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodPost, "/jobs", mustJSON(t, models.CreateJobRequest{
		SourceURI: "s3://x", Pipeline: "ocr",
	}))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var j models.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&j))
	resp.Body.Close()

	ev := do(t, r, http.MethodPost, "/jobs/"+j.ID.String()+"/events",
		bytes.NewReader([]byte(`{"status":"running","message":"pulling"}`)))
	require.Equal(t, http.StatusCreated, ev.StatusCode)
	ev.Body.Close()

	list := do(t, r, http.MethodGet, "/jobs/"+j.ID.String()+"/events", nil)
	require.Equal(t, http.StatusOK, list.StatusCode)
	var out models.ListEventsResponse
	require.NoError(t, json.NewDecoder(list.Body).Decode(&out))
	list.Body.Close()
	require.Len(t, out.Data, 1)
	assert.Equal(t, models.JobStatusRunning, out.Data[0].Status)
}

func TestJobs_RecordExtractionAndListExtractions(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodPost, "/jobs", mustJSON(t, models.CreateJobRequest{
		SourceURI: "s3://x", Pipeline: "ocr",
	}))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var j models.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&j))
	resp.Body.Close()

	ex := do(t, r, http.MethodPost, "/jobs/"+j.ID.String()+"/extractions",
		bytes.NewReader([]byte(`{"extraction_kind":"text","payload":{"chunks":3},"confidence":0.91}`)))
	require.Equal(t, http.StatusCreated, ex.StatusCode)
	ex.Body.Close()

	list := do(t, r, http.MethodGet, "/jobs/"+j.ID.String()+"/extractions", nil)
	require.Equal(t, http.StatusOK, list.StatusCode)
	var out models.ListExtractionsResponse
	require.NoError(t, json.NewDecoder(list.Body).Decode(&out))
	list.Body.Close()
	require.Len(t, out.Data, 1)
	assert.Equal(t, "text", out.Data[0].ExtractionKind)
}

func TestJobs_GetMissing404(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodGet, "/jobs/00000000-0000-0000-0000-000000000000", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

func TestJobs_ListFiltersByStatus(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	for i := 0; i < 3; i++ {
		resp := do(t, r, http.MethodPost, "/jobs", mustJSON(t, models.CreateJobRequest{
			SourceURI: "s3://x", Pipeline: "ocr",
		}))
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		resp.Body.Close()
	}
	resp := do(t, r, http.MethodGet, "/jobs?status=queued", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out models.ListJobsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	resp.Body.Close()
	assert.Len(t, out.Data, 3)
}

func TestJobs_ListRejectsBadStatus(t *testing.T) {
	t.Parallel()
	store := repo.NewMemoryStore()
	r := newRouter(store)
	resp := do(t, r, http.MethodGet, "/jobs?status=bogus", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp.Body.Close()
}

// --- helpers ---------------------------------------------------------------

func mustJSON(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

func do(t *testing.T, h http.Handler, method, path string, body io.Reader) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}
