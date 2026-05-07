package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/serving"
	mlhandlers "github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/handlers"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/model-deployment-service/internal/config"
)

func fakeConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Service.Name = "model-deployment-service"
	cfg.Service.Version = "test"
	cfg.DeploymentRuntime = "fake"
	return cfg
}

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "model-deployment-service", body["service"])
}

func TestDeploymentRoutesMounted(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	for _, path := range []string{
		"/api/v1/deployments",
		"/api/v1/model-deployment/deployments",
	} {
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "path %s should be mounted", path)
	}
}

func TestModelDeploymentServiceCreateAndGetDeployment(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	srv := httptest.NewServer(BuildRouter(cfg, nil))
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model_id":      SeedModelID,
		"name":          "seed-prod",
		"endpoint_path": "/predict/seed",
		"traffic_split": []map[string]any{{"model_version_id": SeedModelVersionID, "allocation": 100}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	resp, err := http.Post(srv.URL+"/api/v1/model-deployment/deployments", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var created map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	id, ok := created["id"].(string)
	require.True(t, ok)

	getResp, err := http.Get(srv.URL + "/api/v1/model-deployment/deployments/" + id)
	require.NoError(t, err)
	defer getResp.Body.Close()
	require.Equal(t, http.StatusOK, getResp.StatusCode)
	var got map[string]any
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&got))
	assert.Equal(t, "seed-prod", got["name"])
}

func TestModelDeploymentServiceRejectsInvalidVersion(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	srv := httptest.NewServer(BuildRouter(cfg, nil))
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model_id":      SeedModelID,
		"name":          "bad-version",
		"endpoint_path": "/predict/bad",
		"traffic_split": []map[string]any{{"model_version_id": "20000000-0000-0000-0000-000000000999", "allocation": 100}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	resp, err := http.Post(srv.URL+"/api/v1/model-deployment/deployments", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDefaultConfigUsesPostgresStoreWhenDatabaseURLPresent(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	cfg.DatabaseURL = "postgres://user:pass@localhost:5432/openfoundry?sslmode=disable"
	cfg.DeploymentRuntime = ""

	h, err := defaultDeploymentHandler(cfg)

	require.NoError(t, err)
	defer h.Pool.Close()
	_, isFakeStore := h.Store.(*mlhandlers.FakeDeploymentStore)
	assert.False(t, isFakeStore, "DATABASE_URL must not silently wire fake store")
	assert.IsType(t, &mlhandlers.PGDeploymentStore{}, h.Store)
	_, isFakeRuntime := h.Runtime.(*serving.FakeDeploymentRuntime)
	assert.False(t, isFakeRuntime, "runtime fake must be explicit")
	assert.IsType(t, serving.UnavailableDeploymentRuntime{}, h.Runtime)
}

func TestExplicitFakeModeUsesFakeStoreAndRuntime(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()

	h, err := defaultDeploymentHandler(cfg)

	require.NoError(t, err)
	assert.IsType(t, &mlhandlers.FakeDeploymentStore{}, h.Store)
	assert.IsType(t, &serving.FakeDeploymentRuntime{}, h.Runtime)
}

func TestRuntimeFactoryUsesHTTPBackendWhenConfigured(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	cfg.DeploymentRuntime = "http"
	cfg.ServingBackendURL = "http://serving-control-plane"

	runtime, err := deploymentRuntimeFromConfig(cfg)

	require.NoError(t, err)
	httpRuntime, ok := runtime.(*serving.HTTPDeploymentRuntime)
	require.True(t, ok)
	assert.Equal(t, "http://serving-control-plane", httpRuntime.BaseURL)
}

func TestRuntimeFactoryRejectsUnsupportedMode(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	cfg.DeploymentRuntime = "mystery"

	runtime, err := deploymentRuntimeFromConfig(cfg)

	require.Error(t, err)
	assert.Nil(t, runtime)
}

func TestRealConfigWithoutDatabaseURLFailsInsteadOfUsingFake(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	cfg.DeploymentRuntime = ""

	_, err := BuildRouterE(cfg, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

func TestModelDeploymentServiceCreateGetUpdateJSONContract(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig()
	srv := httptest.NewServer(BuildRouter(cfg, nil))
	t.Cleanup(srv.Close)

	createBody, err := json.Marshal(map[string]any{
		"model_id":      SeedModelID,
		"name":          "contract-prod",
		"strategy_type": "single",
		"endpoint_path": "/predict/contract",
		"traffic_split": []map[string]any{{"model_version_id": SeedModelVersionID, "label": "champion", "allocation": 25}},
	})
	require.NoError(t, err)
	createResp, err := http.Post(srv.URL+"/api/v1/model-deployment/deployments", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer createResp.Body.Close()
	require.Equal(t, http.StatusOK, createResp.StatusCode)
	created := decodeDeploymentContract(t, createResp.Body)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, SeedModelID.String(), created.ModelID)
	assert.Equal(t, "contract-prod", created.Name)
	assert.Equal(t, "active", created.Status)
	assert.Equal(t, "single", created.StrategyType)
	assert.Equal(t, "/predict/contract", created.EndpointPath)
	assert.Equal(t, "24h", created.MonitoringWindow)
	require.Len(t, created.TrafficSplit, 1)
	assert.Equal(t, SeedModelVersionID.String(), created.TrafficSplit[0].ModelVersionID)
	assert.Equal(t, "champion", created.TrafficSplit[0].Label)
	assert.Equal(t, 100, created.TrafficSplit[0].Allocation)

	getResp, err := http.Get(srv.URL + "/api/v1/model-deployment/deployments/" + created.ID)
	require.NoError(t, err)
	defer getResp.Body.Close()
	require.Equal(t, http.StatusOK, getResp.StatusCode)
	got := decodeDeploymentContract(t, getResp.Body)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "contract-prod", got.Name)

	patchBody := strings.NewReader(`{"name":"contract-paused","status":"paused","endpoint_path":"/predict/contract-v2"}`)
	patchReq, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/model-deployment/deployments/"+created.ID, patchBody)
	require.NoError(t, err)
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	require.NoError(t, err)
	defer patchResp.Body.Close()
	require.Equal(t, http.StatusOK, patchResp.StatusCode)
	updated := decodeDeploymentContract(t, patchResp.Body)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "contract-paused", updated.Name)
	assert.Equal(t, "paused", updated.Status)
	assert.Equal(t, "/predict/contract-v2", updated.EndpointPath)
	assert.Contains(t, updated.CreatedAt, "T")
	assert.Contains(t, updated.UpdatedAt, "T")
}

type deploymentContract struct {
	ID           string `json:"id"`
	ModelID      string `json:"model_id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	StrategyType string `json:"strategy_type"`
	EndpointPath string `json:"endpoint_path"`
	TrafficSplit []struct {
		ModelVersionID string `json:"model_version_id"`
		Label          string `json:"label"`
		Allocation     int    `json:"allocation"`
	} `json:"traffic_split"`
	MonitoringWindow  string  `json:"monitoring_window"`
	BaselineDatasetID *string `json:"baseline_dataset_id"`
	DriftReport       any     `json:"drift_report"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func decodeDeploymentContract(t *testing.T, body io.Reader) deploymentContract {
	t.Helper()
	var got deploymentContract
	require.NoError(t, json.NewDecoder(body).Decode(&got))
	return got
}
