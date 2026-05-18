package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/reconcile"
	"github.com/stretchr/testify/require"
)

func TestBuildReconcilerSelectsHTTPApplierWhenControlPlaneURLExists(t *testing.T) {
	t.Parallel()
	r, err := buildReconcilerFromEnv(testLogger(), mapEnv(map[string]string{
		"INGESTION_CONTROL_PLANE_URL":     "http://control-plane",
		"INGESTION_CONTROL_PLANE_TIMEOUT": "5s",
	}))
	require.NoError(t, err)
	applier, ok := r.Applier.(*reconcile.HTTPApplier)
	require.True(t, ok)
	require.Equal(t, "http://control-plane", applier.BaseURL)
	require.Equal(t, 5*time.Second, applier.HTTPClient.Timeout)
}

func TestBuildReconcilerFailsWithoutControlPlaneURLByDefault(t *testing.T) {
	t.Parallel()
	_, err := buildReconcilerFromEnv(testLogger(), mapEnv(nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "INGESTION_CONTROL_PLANE_URL is required")
}

func TestBuildReconcilerAllowsExplicitNoopMode(t *testing.T) {
	t.Parallel()
	r, err := buildReconcilerFromEnv(testLogger(), mapEnv(map[string]string{
		"INGESTION_RECONCILE_MODE": "noop",
	}))
	require.NoError(t, err)
	_, ok := r.Applier.(*reconcile.LoggingApplier)
	require.True(t, ok)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string {
		if values == nil {
			return ""
		}
		return values[key]
	}
}
