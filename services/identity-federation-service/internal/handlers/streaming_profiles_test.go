package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// streamingProfileRequest wires both the chi {id} URL param and the
// auth claims into the request context in the right order — applying
// the chi RouteCtx last so callers' downstream WithContext() calls
// won't accidentally drop it.
func streamingProfileRequest(r *http.Request, id string, claims *authmw.Claims) *http.Request {
	if claims != nil {
		r = r.WithContext(authmw.ContextWithClaims(r.Context(), claims))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func streamingProfilesWriter() *authmw.Claims {
	return controlPanelClaims("control_panel:write")
}

func streamingProfilesReader() *authmw.Claims {
	return controlPanelClaims("control_panel:read")
}

func createStreamingProfile(t *testing.T, h *ControlPanel, body string) StreamingProfile {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control-panel/streaming-profiles", strings.NewReader(body)).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesWriter()))
	rec := httptest.NewRecorder()
	h.CreateStreamingProfile(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var got StreamingProfile
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	return got
}

func TestStreamingProfilesListEmptyByDefault(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesReader()))
	rec := httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ListStreamingProfilesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 0, resp.Total)
	require.NotNil(t, resp.Items)
	require.Len(t, resp.Items, 0)
}

func TestStreamingProfilesListRequiresReadPermission(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles", nil)
	rec := httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), controlPanelClaims("users:read")))
	rec = httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestStreamingProfilesCreateRequiresWritePermission(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control-panel/streaming-profiles",
		strings.NewReader(`{"name":"x","connector_type":"streaming_kafka"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), controlPanelClaims("control_panel:read")))
	rec := httptest.NewRecorder()
	h.CreateStreamingProfile(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestStreamingProfilesCreateRejectsMissingName(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control-panel/streaming-profiles",
		strings.NewReader(`{"connector_type":"streaming_kafka"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesWriter()))
	rec := httptest.NewRecorder()
	h.CreateStreamingProfile(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "name is required")
}

func TestStreamingProfilesCreateRejectsUnknownConnectorType(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control-panel/streaming-profiles",
		strings.NewReader(`{"name":"profile","connector_type":"streaming_madeup"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesWriter()))
	rec := httptest.NewRecorder()
	h.CreateStreamingProfile(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "connector_type")
}

func TestStreamingProfilesCreateRejectsNonObjectSourceConfig(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control-panel/streaming-profiles",
		strings.NewReader(`{"name":"p","connector_type":"streaming_kafka","source_config":["bootstrap"]}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesWriter()))
	rec := httptest.NewRecorder()
	h.CreateStreamingProfile(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "source_config must be a JSON object")
}

func TestStreamingProfilesCreateStampsAuditFields(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	profile := createStreamingProfile(t, h,
		`{"name":"Kafka warm","connector_type":"streaming_kafka","parallelism":4,"watermark_policy":"bounded_out_of_orderness","source_config":{"topic":"events"}}`)
	require.NotEmpty(t, profile.ID)
	require.Equal(t, "Kafka warm", profile.Name)
	require.Equal(t, "streaming_kafka", profile.ConnectorType)
	require.Equal(t, StreamingProfileStatusDraft, profile.Status)
	require.Equal(t, "bounded_out_of_orderness", profile.WatermarkPolicy)
	require.Equal(t, uint32(4), profile.Parallelism)
	require.NotNil(t, profile.CreatedBy)
	require.Equal(t, "admin@example.com", *profile.CreatedBy)
	require.NotNil(t, profile.CreatedAt)
	require.NotNil(t, profile.UpdatedBy)
}

func TestStreamingProfilesCreateRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	createStreamingProfile(t, h, `{"name":"dup","connector_type":"streaming_kafka"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/control-panel/streaming-profiles",
		strings.NewReader(`{"name":"dup","connector_type":"streaming_kinesis"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesWriter()))
	rec := httptest.NewRecorder()
	h.CreateStreamingProfile(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestStreamingProfilesListReturnsCreatedItem(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	createStreamingProfile(t, h, `{"name":"Beta","connector_type":"streaming_kinesis"}`)
	createStreamingProfile(t, h, `{"name":"Alpha","connector_type":"streaming_kafka"}`)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesReader()))
	rec := httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ListStreamingProfilesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 2, resp.Total)
	require.Equal(t, "Alpha", resp.Items[0].Name)
	require.Equal(t, "Beta", resp.Items[1].Name)
}

func TestStreamingProfilesListFiltersByStatusAndConnector(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	createStreamingProfile(t, h, `{"name":"k1","connector_type":"streaming_kafka","status":"active"}`)
	createStreamingProfile(t, h, `{"name":"k2","connector_type":"streaming_kafka","status":"paused"}`)
	createStreamingProfile(t, h, `{"name":"x1","connector_type":"streaming_kinesis","status":"active"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles?status=active", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesReader()))
	rec := httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ListStreamingProfilesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 2, resp.Total)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles?connector_type=streaming_kafka&status=paused", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesReader()))
	rec = httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 1, resp.Total)
	require.Equal(t, "k2", resp.Items[0].Name)
}

func TestStreamingProfilesGetReturns404ForMissing(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := streamingProfileRequest(
		httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles/missing", nil),
		"missing", streamingProfilesReader())
	rec := httptest.NewRecorder()
	h.GetStreamingProfile(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStreamingProfilesUpdateMergesPartialFields(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	created := createStreamingProfile(t, h,
		`{"name":"orig","connector_type":"streaming_kafka","parallelism":2,"watermark_policy":"none"}`)

	req := streamingProfileRequest(
		httptest.NewRequest(http.MethodPatch, "/api/v1/control-panel/streaming-profiles/"+created.ID,
			strings.NewReader(`{"parallelism":8,"description":"updated desc"}`)),
		created.ID, streamingProfilesWriter())
	rec := httptest.NewRecorder()
	h.UpdateStreamingProfile(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var updated StreamingProfile
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&updated))
	require.Equal(t, "orig", updated.Name)
	require.Equal(t, uint32(8), updated.Parallelism)
	require.Equal(t, "updated desc", updated.Description)
	require.Equal(t, "none", updated.WatermarkPolicy)
	require.NotNil(t, updated.UpdatedAt)
}

func TestStreamingProfilesUpdateRejectsUnknownWatermark(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	created := createStreamingProfile(t, h, `{"name":"w","connector_type":"streaming_kafka"}`)

	req := streamingProfileRequest(
		httptest.NewRequest(http.MethodPatch, "/api/v1/control-panel/streaming-profiles/"+created.ID,
			strings.NewReader(`{"watermark_policy":"made_up"}`)),
		created.ID, streamingProfilesWriter())
	rec := httptest.NewRecorder()
	h.UpdateStreamingProfile(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "watermark_policy")
}

func TestStreamingProfilesPauseAndResumeAreIdempotent(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	created := createStreamingProfile(t, h, `{"name":"p","connector_type":"streaming_kafka","status":"active"}`)

	for i := 0; i < 2; i++ {
		req := streamingProfileRequest(
			httptest.NewRequest(http.MethodPost,
				"/api/v1/control-panel/streaming-profiles/"+created.ID+":pause", nil),
			created.ID, streamingProfilesWriter())
		rec := httptest.NewRecorder()
		h.PauseStreamingProfile(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "iteration %d: %s", i, rec.Body.String())
		var got StreamingProfile
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
		require.Equal(t, StreamingProfileStatusPaused, got.Status)
	}

	req := streamingProfileRequest(
		httptest.NewRequest(http.MethodPost,
			"/api/v1/control-panel/streaming-profiles/"+created.ID+":resume", nil),
		created.ID, streamingProfilesWriter())
	rec := httptest.NewRecorder()
	h.ResumeStreamingProfile(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var got StreamingProfile
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Equal(t, StreamingProfileStatusActive, got.Status)
}

func TestStreamingProfilesResumeRefusesErrorState(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	created := createStreamingProfile(t, h, `{"name":"e","connector_type":"streaming_kafka","status":"error"}`)

	req := streamingProfileRequest(
		httptest.NewRequest(http.MethodPost,
			"/api/v1/control-panel/streaming-profiles/"+created.ID+":resume", nil),
		created.ID, streamingProfilesWriter())
	rec := httptest.NewRecorder()
	h.ResumeStreamingProfile(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "error state")
}

func TestStreamingProfilesDeleteRemovesFromList(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	created := createStreamingProfile(t, h, `{"name":"d","connector_type":"streaming_kafka"}`)

	req := streamingProfileRequest(
		httptest.NewRequest(http.MethodDelete, "/api/v1/control-panel/streaming-profiles/"+created.ID, nil),
		created.ID, streamingProfilesWriter())
	rec := httptest.NewRecorder()
	h.DeleteStreamingProfile(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/control-panel/streaming-profiles", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), streamingProfilesReader()))
	rec = httptest.NewRecorder()
	h.ListStreamingProfiles(rec, req)
	var resp ListStreamingProfilesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 0, resp.Total)
}
