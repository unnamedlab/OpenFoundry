package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/config"
)

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "federation-product-exchange-service"
	cfg.Service.Version = "test"
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "federation-product-exchange-service", body["service"])
}

func TestNoSubdomainRoutesYet(t *testing.T) {
	// Wire-compat with Rust: the binary is `fn main(){}` with the
	// three sub-domain modules dead-code annotated. No /api/v1/* is
	// mounted yet — the follow-up slices wire each sub-domain.
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "federation-product-exchange-service"
	cfg.Service.Version = "test"
	srv := httptest.NewServer(BuildRouter(cfg, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	for _, path := range []string{
		"/api/v1/marketplace/listings",
		"/api/v1/marketplace-catalog/browse",
		"/api/v1/product-distribution/peers",
		"/api/v1/nexus/overview",
	} {
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "path %s should not be mounted yet", path)
	}
}
