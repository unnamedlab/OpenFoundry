package cassandrakernel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestSchemaStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ repos.SchemaStore = (*SchemaStore)(nil)
}

func TestSchemaVersionToCQLRejectsZero(t *testing.T) {
	t.Parallel()
	_, err := schemaVersionToCQL(0)
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err))
	assert.Contains(t, err.Error(), "must be greater than zero")
}

func TestSchemaVersionToCQLAcceptsRangeBoundaries(t *testing.T) {
	t.Parallel()
	for _, v := range []uint32{1, 7, 100, 1<<31 - 1} {
		got, err := schemaVersionToCQL(v)
		require.NoError(t, err, "version=%d", v)
		assert.Equal(t, int32(v), got)
	}
}

func TestSchemaVersionToCQLRejectsOverflow(t *testing.T) {
	t.Parallel()
	_, err := schemaVersionToCQL(1 << 31) // first value above MaxInt32
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err))
	assert.Contains(t, err.Error(), "exceeds CQL int range")
}

func TestSchemaVersionFromCQLNegativeIsBackend(t *testing.T) {
	t.Parallel()
	_, err := schemaVersionFromCQL(-3)
	require.Error(t, err)
	assert.True(t, repos.IsBackendError(err))
	assert.Contains(t, err.Error(), "stored schema version is negative: -3")
}

func TestSchemaVersionFromCQLNonNegative(t *testing.T) {
	t.Parallel()
	got, err := schemaVersionFromCQL(42)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), got)
}

func TestSchemaCQLStatementsReferenceKeyspace(t *testing.T) {
	t.Parallel()
	s := &SchemaStore{keyspace: "ontology_objects"}
	assert.Contains(t, s.cqlInsertVersion(), "ontology_objects.schemas_by_type")
	assert.Contains(t, s.cqlInsertLatest(), "ontology_objects.schemas_latest")
	assert.Contains(t, s.cqlSelectLatest(), "FROM ontology_objects.schemas_latest")
	assert.Contains(t, s.cqlSelectVersion(), "FROM ontology_objects.schemas_by_type")
	assert.Contains(t, s.cqlInsertVersion(), "IF NOT EXISTS")
	assert.Contains(t, s.cqlInsertLatest(), "IF NOT EXISTS")
	assert.Contains(t, s.cqlUpdateLatestIfVersion(), "IF version = ?")
}
