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

func TestNoProviderRoutesYet(t *testing.T) {
	// Wire-compat: Rust binary is `fn main(){}`. No /api/v1/* yet.
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "llm-catalog-service"
	cfg.Service.Version = "test"
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	for _, path := range []string{"/api/v1/providers", "/v1/providers"} {
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "path %s should not be mounted yet", path)
	}
}
