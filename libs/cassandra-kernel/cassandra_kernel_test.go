package cassandrakernel_test

import (
	"testing"

	"github.com/gocql/gocql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cassandrakernel "github.com/openfoundry/openfoundry-go/libs/cassandra-kernel"
)

func TestFromEnvRequiresContactPoints(t *testing.T) {
	// Cannot t.Parallel — env mutation.
	t.Setenv("CASSANDRA_CONTACT_POINTS", "")
	_, err := cassandrakernel.FromEnv()
	require.Error(t, err)
	assert.True(t, cassandrakernel.IsMissingEnv(err))
}

func TestFromEnvParsesHostsAndDefaults(t *testing.T) {
	t.Setenv("CASSANDRA_CONTACT_POINTS", "scylla-0:9042 , scylla-1:9042")
	t.Setenv("CASSANDRA_KEYSPACE", "auth_runtime")
	c, err := cassandrakernel.FromEnv()
	require.NoError(t, err)
	assert.Equal(t, []string{"scylla-0:9042", "scylla-1:9042"}, c.Hosts)
	assert.Equal(t, "auth_runtime", c.Keyspace)
	assert.Equal(t, "dc1", c.Datacenter, "default LOCAL_DC=dc1")
	assert.Equal(t, gocql.LocalQuorum, c.Consistency, "default LOCAL_QUORUM")
}

func TestFromEnvHonoursConsistency(t *testing.T) {
	t.Setenv("CASSANDRA_CONTACT_POINTS", "scylla:9042")
	t.Setenv("CASSANDRA_CONSISTENCY", "QUORUM")
	c, err := cassandrakernel.FromEnv()
	require.NoError(t, err)
	assert.Equal(t, gocql.Quorum, c.Consistency)
}
