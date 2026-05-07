package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/model-deployment-service/internal/config"
)

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "model-deployment-service"
	cfg.Service.Version = "test"
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
	cfg := &config.Config{}
	cfg.Service.Name = "model-deployment-service"
	cfg.Service.Version = "test"
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
	cfg := &config.Config{}
	cfg.Service.Name = "model-deployment-service"
	cfg.Service.Version = "test"
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
	cfg := &config.Config{}
	cfg.Service.Name = "model-deployment-service"
	cfg.Service.Version = "test"
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
