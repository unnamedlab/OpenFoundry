package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/predictions"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestRealtimePredict_RejectsEmptyInputs(t *testing.T) {
	t.Parallel()
	h := &PredictionsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"inputs":[]}`))
	w := httptest.NewRecorder()
	h.RealtimePredict(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "prediction inputs are required", body.Error)
}

func TestCreateBatchPrediction_RejectsEmptyRecords(t *testing.T) {
	t.Parallel()
	h := &PredictionsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"deployment_id":"`+uuid.New().String()+`","records":[]}`))
	w := httptest.NewRecorder()
	h.CreateBatchPrediction(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "batch prediction records are required", body.Error)
}

func TestCreateBatchPrediction_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &PredictionsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateBatchPrediction(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRunPredictions_DropsRecordsWithoutMatchingRuntime(t *testing.T) {
	t.Parallel()
	splitID := uuid.New()
	splits := []models.TrafficSplitEntry{
		{ModelVersionID: splitID, Label: "champion", Allocation: 100},
	}
	versions := map[uuid.UUID]predictions.ModelRuntime{
		splitID: {VersionNumber: 1, Schema: map[string]any{}},
	}
	inputs := []json.RawMessage{
		json.RawMessage(`{"feature_a":1}`),
		json.RawMessage(`{"feature_b":2}`),
	}
	out := runPredictions(inputs, splits, versions, true)
	require.Len(t, out, 2)
	assert.Equal(t, "record-1", out[0].RecordID)
	assert.Equal(t, "record-2", out[1].RecordID)
	assert.Equal(t, "champion", out[0].Variant)
}

func TestRunPredictions_EmptySplitsReturnsEmpty(t *testing.T) {
	t.Parallel()
	out := runPredictions(
		[]json.RawMessage{json.RawMessage(`{}`)},
		nil,
		map[uuid.UUID]predictions.ModelRuntime{},
		false,
	)
	assert.Empty(t, out)
}
