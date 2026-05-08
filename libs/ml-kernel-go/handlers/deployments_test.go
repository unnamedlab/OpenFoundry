package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/serving"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestCreateDeployment_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &DeploymentsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"  ","endpoint_path":"/api/v1/predict"}`))
	w := httptest.NewRecorder()
	h.CreateDeployment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "deployment name and endpoint path are required", body.Error)
}

func TestCreateDeployment_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &DeploymentsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateDeployment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNormaliseTrafficSplit_RequiresAtLeastOneEntry(t *testing.T) {
	t.Parallel()
	_, err := normaliseTrafficSplit("ab_test", nil)
	require.Error(t, err)
	assert.Equal(t, "at least one traffic split entry is required", err.Error())
}

func TestNormaliseTrafficSplit_SingleStrategyCollapses(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	got, err := normaliseTrafficSplit("single", []models.TrafficSplitEntry{
		{ModelVersionID: id, Allocation: 30},
		{ModelVersionID: uuid.New(), Allocation: 70},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, uint8(100), got[0].Allocation)
	assert.Equal(t, id, got[0].ModelVersionID)
}

func TestNormaliseTrafficSplit_AssignsDefaultLabel(t *testing.T) {
	t.Parallel()
	got, err := normaliseTrafficSplit("ab_test", []models.TrafficSplitEntry{
		{ModelVersionID: uuid.New(), Label: "  ", Allocation: 50},
		{ModelVersionID: uuid.New(), Label: "champion", Allocation: 50},
	})
	require.NoError(t, err)
	assert.Equal(t, "variant-1", got[0].Label)
	assert.Equal(t, "champion", got[1].Label)
}

func TestNormaliseTrafficSplit_ABTestRebalancesTo100(t *testing.T) {
	t.Parallel()
	got, err := normaliseTrafficSplit("ab_test", []models.TrafficSplitEntry{
		{ModelVersionID: uuid.New(), Label: "a", Allocation: 30},
		{ModelVersionID: uuid.New(), Label: "b", Allocation: 30},
		{ModelVersionID: uuid.New(), Label: "c", Allocation: 30},
	})
	require.NoError(t, err)
	var total uint32
	for _, s := range got {
		total += uint32(s.Allocation)
	}
	assert.Equal(t, uint32(100), total, "ab_test allocations always sum to 100")
}

func TestNormaliseTrafficSplit_ABTestRejectsZeroTotal(t *testing.T) {
	t.Parallel()
	_, err := normaliseTrafficSplit("ab_test", []models.TrafficSplitEntry{
		{ModelVersionID: uuid.New(), Label: "a", Allocation: 0},
	})
	require.Error(t, err)
	assert.Equal(t, "traffic allocation must be greater than zero", err.Error())
}

func seededDeploymentHandler(t *testing.T) (*DeploymentsHandlers, uuid.UUID, uuid.UUID, *serving.FakeDeploymentRuntime) {
	t.Helper()
	modelID := uuid.New()
	versionID := uuid.New()
	store := NewFakeDeploymentStore()
	store.SeedModel(models.RegisteredModel{ID: modelID, Name: "fraud", ProblemType: models.DefaultProblemType, Status: "active"},
		models.ModelVersion{ID: versionID, ModelID: modelID, VersionNumber: 1, VersionLabel: "v1", Stage: "production"})
	runtime := serving.NewFakeDeploymentRuntime()
	return &DeploymentsHandlers{Store: store, Runtime: runtime}, modelID, versionID, runtime
}

func TestCreateDeployment_WithInjectedStoreAndRuntime(t *testing.T) {
	t.Parallel()
	h, modelID, versionID, runtime := seededDeploymentHandler(t)
	body := fmt.Sprintf(`{"model_id":"%s","name":"fraud-prod","endpoint_path":"/predict/fraud","traffic_split":[{"model_version_id":"%s","allocation":25}]}`, modelID, versionID)
	w := httptest.NewRecorder()
	h.CreateDeployment(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code)
	var deployment models.ModelDeployment
	require.NoError(t, json.NewDecoder(w.Body).Decode(&deployment))
	assert.Equal(t, modelID, deployment.ModelID)
	assert.Equal(t, "active", deployment.Status)
	require.Len(t, deployment.TrafficSplit, 1)
	assert.Equal(t, uint8(100), deployment.TrafficSplit[0].Allocation)
	require.Len(t, runtime.Deployments, 1)
}

func TestGetDeployment_WithInjectedStore(t *testing.T) {
	t.Parallel()
	h, modelID, versionID, _ := seededDeploymentHandler(t)
	body := fmt.Sprintf(`{"model_id":"%s","name":"fraud-prod","endpoint_path":"/predict/fraud","traffic_split":[{"model_version_id":"%s","allocation":100}]}`, modelID, versionID)
	createW := httptest.NewRecorder()
	h.CreateDeployment(createW, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, createW.Code)
	var created models.ModelDeployment
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&created))

	getW := httptest.NewRecorder()
	h.GetDeployment(getW, httptest.NewRequest(http.MethodGet, "/", nil), created.ID)
	require.Equal(t, http.StatusOK, getW.Code)
	var got models.ModelDeployment
	require.NoError(t, json.NewDecoder(getW.Body).Decode(&got))
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "fraud-prod", got.Name)
}

func TestCreateDeployment_InvalidModelVersion(t *testing.T) {
	t.Parallel()
	h, modelID, _, _ := seededDeploymentHandler(t)
	body := fmt.Sprintf(`{"model_id":"%s","name":"fraud-prod","endpoint_path":"/predict/fraud","traffic_split":[{"model_version_id":"%s","allocation":100}]}`, modelID, uuid.New())
	w := httptest.NewRecorder()
	h.CreateDeployment(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errBody ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&errBody))
	assert.Equal(t, "model version not found for model", errBody.Error)
}

func TestCreateDeployment_RuntimeUnavailable(t *testing.T) {
	t.Parallel()
	h, modelID, versionID, runtime := seededDeploymentHandler(t)
	runtime.Available = false
	body := fmt.Sprintf(`{"model_id":"%s","name":"fraud-prod","endpoint_path":"/predict/fraud","traffic_split":[{"model_version_id":"%s","allocation":100}]}`, modelID, versionID)
	w := httptest.NewRecorder()
	h.CreateDeployment(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var errBody ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&errBody))
	assert.Equal(t, serving.ErrRuntimeUnavailable.Error(), errBody.Error)
}

func TestUpdateDeployment_StatusTransition(t *testing.T) {
	t.Parallel()
	h, modelID, versionID, runtime := seededDeploymentHandler(t)
	body := fmt.Sprintf(`{"model_id":"%s","name":"fraud-prod","endpoint_path":"/predict/fraud","traffic_split":[{"model_version_id":"%s","allocation":100}]}`, modelID, versionID)
	createW := httptest.NewRecorder()
	h.CreateDeployment(createW, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, createW.Code)
	var created models.ModelDeployment
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&created))

	patchW := httptest.NewRecorder()
	h.UpdateDeployment(patchW, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"status":"paused"}`)), created.ID)
	require.Equal(t, http.StatusOK, patchW.Code)
	var updated models.ModelDeployment
	require.NoError(t, json.NewDecoder(patchW.Body).Decode(&updated))
	assert.Equal(t, "paused", updated.Status)
	require.Len(t, runtime.Transitions, 1)
	assert.Equal(t, created.ID, runtime.Transitions[0].DeploymentID)
	assert.Equal(t, "paused", runtime.Transitions[0].Status)
}

func TestCreateDeployment_InvalidModel(t *testing.T) {
	t.Parallel()
	h, _, versionID, _ := seededDeploymentHandler(t)
	body := fmt.Sprintf(`{"model_id":"%s","name":"fraud-prod","endpoint_path":"/predict/fraud","traffic_split":[{"model_version_id":"%s","allocation":100}]}`, uuid.New(), versionID)
	w := httptest.NewRecorder()
	h.CreateDeployment(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errBody ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&errBody))
	assert.Equal(t, "model not found", errBody.Error)
}
