package idempotency

// Internal test (package idempotency, not _test) so we can reach
// CassandraStore.cqlInsert and assert the LWT shape without a live
// Cassandra session — same pattern cassandra-kernel/link_store_test
// uses for its IF-NOT-EXISTS assertion.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCassandraStoreCQLIsLWT(t *testing.T) {
	t.Parallel()
	// Real session not required for the CQL-shape assertion.
	s := &CassandraStore{ksTable: "idem.processed_events"}
	cql := s.cqlInsert()

	require.True(t, strings.HasPrefix(cql, "INSERT INTO idem.processed_events"),
		"table slot must be interpolated literally: %q", cql)
	assert.Contains(t, cql, "(event_id, processed_at)")
	assert.Contains(t, cql, "VALUES (?, ?)")
	assert.Contains(t, cql, "IF NOT EXISTS",
		"INSERT must be an LWT — Cassandra needs IF NOT EXISTS for "+
			"atomic check-and-record")
}

func TestNewCassandraStorePanicsOnEmptyTable(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on empty ksTable")
		}
	}()
	// session is nil too but the empty-table check fires first.
	NewCassandraStore(nil, "")
}

func TestCassandraStoreAccessorsReturnConstructorValues(t *testing.T) {
	t.Parallel()
	s := &CassandraStore{ksTable: "audit.processed_events"}
	assert.Equal(t, "audit.processed_events", s.KsTable())
	assert.Nil(t, s.Session(), "no session was wired in for this construction style")
}
