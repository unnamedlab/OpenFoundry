package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromEnvParsesProductionCassandraConfig(t *testing.T) {
	t.Setenv("CASSANDRA_CONTACT_POINTS", "cass1:9042, cass2:9042")
	t.Setenv("CASSANDRA_KEYSPACE", "ontology_runtime")
	t.Setenv("CASSANDRA_USERNAME", "svc")
	t.Setenv("CASSANDRA_PASSWORD", "secret")
	t.Setenv("CASSANDRA_LOCAL_DC", "dc2")

	cfg, err := FromEnv()
	require.NoError(t, err)
	assert.Equal(t, BackendCassandra, cfg.Backend)
	assert.False(t, cfg.DevMode)
	assert.Equal(t, []string{"cass1:9042", "cass2:9042"}, cfg.CassandraPoints())
	assert.Equal(t, "ontology_runtime", cfg.CassandraObjectKeyspace)
	assert.Equal(t, "ontology_runtime", cfg.CassandraLinkKeyspace)
	assert.Equal(t, "svc", cfg.CassandraUsername)
	assert.Equal(t, "secret", cfg.CassandraPassword)
	assert.Equal(t, "dc2", cfg.CassandraLocalDC)
}

func TestFromEnvParsesExplicitDevInMemory(t *testing.T) {
	t.Setenv("OF_DEV_STUB_MODE", "true")
	t.Setenv("OBJECT_DATABASE_BACKEND", "in_memory")

	cfg, err := FromEnv()
	require.NoError(t, err)
	assert.True(t, cfg.DevMode)
	assert.Equal(t, BackendInMemory, cfg.Backend)
	assert.Equal(t, "ontology_objects", cfg.CassandraObjectKeyspace)
	assert.Equal(t, "ontology_indexes", cfg.CassandraLinkKeyspace)
}
