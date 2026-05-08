package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/config"
)

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "llm-catalog-service"
	cfg.Service.Version = "test"
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "llm-catalog-service", body["service"])
}

func TestKernelDefaultsRouteUsesAiKernelModels(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "llm-catalog-service"
	cfg.Service.Version = "test"
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/kernel-defaults")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "openai", body["provider_type"])
	assert.Equal(t, "gpt-4.1-mini", body["model_name"])
	assert.Equal(t, "simulated", body["tool_execution_mode"])
	assert.Equal(t, true, body["fallback_enabled"])
	routeRules := body["route_rules"].(map[string]any)
	assert.Equal(t, "public", routeRules["network_scope"])
}
