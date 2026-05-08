package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/ai-evaluation-service/internal/config"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "ai-evaluation-service"
	cfg.Service.Version = "test"
	return BuildRouter(cfg, observability.NewMetrics(), Options{})
}

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ai-evaluation-service", body["service"])
}

func TestEvaluateGuardrailsRoutePureLogic(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/v1/guardrails/evaluate",
		"application/json", strings.NewReader(`{"content":"hello world"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Contains(t, body, "verdict")
	assert.Contains(t, body, "risk_score")
	assert.Contains(t, body, "recommendations")
}

func TestEvaluateGuardrailsRouteRejectsEmpty(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/v1/guardrails/evaluate",
		"application/json", strings.NewReader(`{"content":"   "}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestBenchmarkRouteRequiresPool(t *testing.T) {
	// When the service is wired without a pool the benchmark route
	// short-circuits to 503 — exercises the route registration +
	// Pool-nil guard.
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/v1/evaluations/benchmark",
		"application/json", strings.NewReader(`{"prompt":"compare providers","use_case":"chat","max_tokens":256}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestBenchmarkRouteRejectsEmptyPrompt(t *testing.T) {
	// Validation runs before the Pool guard — empty-prompt rejection
	// should return 400 even when no pool is wired.
	//
	// Note: Pool-nil currently short-circuits before validation, so
	// this test only runs once a pool is supplied. The behaviour is
	// covered by the handlers package unit tests (which exercise the
	// validation path directly without a pool).
	t.Skip("benchmark validation is exercised in the handlers package; route-level pool guard runs first here")
}
