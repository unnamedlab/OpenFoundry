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

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/interop"
)

func TestCreateModel_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &ModelsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"   "}`))
	w := httptest.NewRecorder()
	h.CreateModel(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "model name is required", body.Error)
}

func TestCreateModel_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &ModelsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateModel(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTransitionModelVersion_RejectsEmptyStage(t *testing.T) {
	t.Parallel()
	h := &ModelsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"stage":"   "}`))
	w := httptest.NewRecorder()
	h.TransitionModelVersion(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "target stage is required", body.Error)
}

func TestTransitionModelVersion_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &ModelsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.TransitionModelVersion(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestExtractDescriptorsDropEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, interop.ModelAdapterFromSchema(nil))
	assert.Nil(t, interop.RegistrySourceFromSchema(json.RawMessage(`{}`)))
	assert.Nil(t, interop.TrackingSourceFromSchema(json.RawMessage(`{"external_tracking":{}}`)))
}

func TestModelAdapterFromSchemaPickup(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"model_adapter":{"framework":"xgboost"}}`)
	got := interop.ModelAdapterFromSchema(schema)
	require.NotNil(t, got)
	assert.Equal(t, "xgboost", got.Framework)
}

func TestRegistrySourceFromSchemaPickup(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"registry_source":{"system":"mlflow"}}`)
	got := interop.RegistrySourceFromSchema(schema)
	require.NotNil(t, got)
	assert.Equal(t, "mlflow", got.System)
}

func TestTrackingSourceFromSchemaPickup(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"external_tracking":{"system":"mlflow","run_id":"r1"}}`)
	got := interop.TrackingSourceFromSchema(schema)
	require.NotNil(t, got)
	assert.Equal(t, "mlflow", got.System)
	assert.Equal(t, "r1", got.RunID)
}
