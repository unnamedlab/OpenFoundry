package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/interop"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestCreateTrainingJob_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &TrainingHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"   "}`))
	w := httptest.NewRecorder()
	h.CreateTrainingJob(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "training job name is required", body.Error)
}

// CreateTrainingJob with valid input now goes to ExecuteTraining
// and tries to insert into the DB; with nil pool it panics. Test
// only the input-validation path (kept above) — DB-bound paths are
// covered by the consuming service's integration tests.

func TestCreateTrainingJob_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &TrainingHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateTrainingJob(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrackingSourceFromTrainingConfigDropsEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, interop.TrackingSourceFromTrainingConfig(nil))
	assert.Nil(t, interop.TrackingSourceFromTrainingConfig(json.RawMessage(`{}`)))
	assert.Nil(t, interop.TrackingSourceFromTrainingConfig(json.RawMessage(`{"external_training":{}}`)),
		"empty fields → HasSignal=false → nil")
}

func TestTrackingSourceFromTrainingConfigPicksUpSignal(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"external_training":{"system":"mlflow","run_id":"run-42"}}`)
	got := interop.TrackingSourceFromTrainingConfig(cfg)
	require.NotNil(t, got)
	assert.Equal(t, "mlflow", got.System)
	assert.Equal(t, "run-42", got.RunID)
}

func TestTrackingSourceFromTrainingConfigBadJSON(t *testing.T) {
	t.Parallel()
	assert.Nil(t, interop.TrackingSourceFromTrainingConfig(json.RawMessage(`not-json`)))
}

func TestExternalTrackingSourceHasSignalRoundtrip(t *testing.T) {
	t.Parallel()
	src := models.ExternalTrackingSource{System: "mlflow"}
	assert.True(t, src.HasSignal())
	empty := models.ExternalTrackingSource{}
	assert.False(t, empty.HasSignal())
}
