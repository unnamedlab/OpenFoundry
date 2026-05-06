package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBackendKindFromEnv(t *testing.T) {
	t.Parallel()
	assert.Equal(t, BackendVespa, BackendKindFromEnv(""))
	assert.Equal(t, BackendVespa, BackendKindFromEnv("vespa"))
	assert.Equal(t, BackendVespa, BackendKindFromEnv("  VESPA  "))
	assert.Equal(t, BackendVespa, BackendKindFromEnv("unknown"))
	assert.Equal(t, BackendOpenSearch, BackendKindFromEnv("opensearch"))
	assert.Equal(t, BackendOpenSearch, BackendKindFromEnv("OpenSearch"))
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("SEARCH_BACKEND", "")
	t.Setenv("SEARCH_ENDPOINT", "")
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "")
	t.Setenv("KAFKA_CONSUMER_GROUP", "")
	t.Setenv("METRICS_ADDR", "")

	cfg, err := FromEnv()
	assert.NoError(t, err)
	assert.Equal(t, "ontology-indexer", cfg.Service.Name)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, uint16(50124), cfg.Server.Port)
	assert.Equal(t, BackendVespa, cfg.BackendKind)
	assert.Equal(t, "ontology-indexer", cfg.ConsumerGroup)
	assert.Equal(t, "0.0.0.0:9090", cfg.MetricsAddr)
}
