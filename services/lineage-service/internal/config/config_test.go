package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuntimeModeFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want RuntimeMode
	}{
		{"explicit kafka", map[string]string{"LINEAGE_RUNTIME_MODE": "kafka"}, ModeKafkaToIceberg},
		{"explicit kafka_to_iceberg", map[string]string{"LINEAGE_RUNTIME_MODE": "kafka_to_iceberg"}, ModeKafkaToIceberg},
		{"explicit iceberg", map[string]string{"LINEAGE_RUNTIME_MODE": "iceberg"}, ModeKafkaToIceberg},
		{"explicit http", map[string]string{"LINEAGE_RUNTIME_MODE": "http"}, ModeHTTPHealth},
		{"explicit http_health", map[string]string{"LINEAGE_RUNTIME_MODE": "http_health"}, ModeHTTPHealth},
		{"infer kafka via env presence", map[string]string{"ICEBERG_CATALOG_URL": "x", "KAFKA_BOOTSTRAP_SERVERS": "y"}, ModeKafkaToIceberg},
		{"only iceberg url → http", map[string]string{"ICEBERG_CATALOG_URL": "x"}, ModeHTTPHealth},
		{"only kafka bootstrap → http", map[string]string{"KAFKA_BOOTSTRAP_SERVERS": "y"}, ModeHTTPHealth},
		{"unset → http", map[string]string{}, ModeHTTPHealth},
		{"trims + casefold", map[string]string{"LINEAGE_RUNTIME_MODE": "  KaFkA  "}, ModeKafkaToIceberg},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("LINEAGE_RUNTIME_MODE", "")
			t.Setenv("ICEBERG_CATALOG_URL", "")
			t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "")
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			assert.Equal(t, c.want, RuntimeModeFromEnv())
		})
	}
}

func TestFromEnvDefaults(t *testing.T) {
	for _, k := range []string{
		"HOST", "PORT", "DATABASE_URL", "JWT_SECRET", "DATA_DIR",
		"DATASET_SERVICE_URL", "WORKFLOW_SERVICE_URL", "AI_SERVICE_URL",
		"STORAGE_BACKEND", "STORAGE_BUCKET",
	} {
		t.Setenv(k, "")
	}
	cfg, err := FromEnv()
	assert.NoError(t, err)
	assert.Equal(t, "lineage-service", cfg.Service.Name)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, uint16(50083), cfg.Server.Port, "default port matches Rust 50083")
	assert.Equal(t, "/tmp/pipeline-data", cfg.DataDir)
	assert.Equal(t, "http://localhost:50079", cfg.DatasetServiceURL)
	assert.Equal(t, "http://localhost:50137", cfg.WorkflowServiceURL)
	assert.Equal(t, "http://localhost:50127", cfg.AIServiceURL)
	assert.Equal(t, "s3", cfg.StorageBackend)
	assert.Equal(t, "datasets", cfg.StorageBucket)
	assert.Equal(t, uint32(1), cfg.DistributedPipelineWorkers)
	assert.Equal(t, uint64(5000), cfg.DistributedComputePollIntervalMs)
	assert.Equal(t, uint64(900), cfg.DistributedComputeTimeoutSecs)
}
