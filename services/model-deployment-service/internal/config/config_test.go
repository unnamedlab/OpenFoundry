package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromEnvReadsDeploymentRuntimeConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("OF_MODEL_DEPLOYMENT_RUNTIME", "http")
	t.Setenv("OF_MODEL_SERVING_BACKEND_URL", "http://serving")

	cfg, err := FromEnv()

	require.NoError(t, err)
	assert.Equal(t, "postgres://db", cfg.DatabaseURL)
	assert.Equal(t, "http", cfg.DeploymentRuntime)
	assert.Equal(t, "http://serving", cfg.ServingBackendURL)
}

func TestFromEnvAcceptsLegacyServingBackendURL(t *testing.T) {
	t.Setenv("MODEL_SERVING_BACKEND_URL", "http://legacy-serving")

	cfg, err := FromEnv()

	require.NoError(t, err)
	assert.Equal(t, "http://legacy-serving", cfg.ServingBackendURL)
}
