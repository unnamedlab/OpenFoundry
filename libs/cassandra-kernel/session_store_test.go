package cassandrakernel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestSessionStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ repos.SessionStore = (*SessionStore)(nil)
}

func TestTTLSecondsExpiredReturnsFalse(t *testing.T) {
	t.Parallel()
	now := int64(1_700_000_000_000)
	expired := now - 1
	_, ok := ttlSecondsUntil(expired, now)
	assert.False(t, ok, "already-expired session must signal short-circuit")

	atExpiry := now
	_, ok = ttlSecondsUntil(atExpiry, now)
	assert.False(t, ok, "exactly-now expiry counts as expired")
}

func TestTTLSecondsRoundsUpToNextSecond(t *testing.T) {
	t.Parallel()
	now := int64(1_700_000_000_000)
	cases := []struct {
		ttlMs    int64
		expected int32
	}{
		{1, 1},     // 1 ms → 1 second
		{500, 1},   // 0.5 s → 1 second
		{999, 1},   // 0.999 s → 1 second
		{1000, 1},  // exactly 1 second → 1
		{1001, 2},  // 1.001 s → 2 seconds (round up)
		{60_000, 60},
		{86_400_000, 86_400},
	}
	for _, c := range cases {
		got, ok := ttlSecondsUntil(now+c.ttlMs, now)
		require.True(t, ok)
		assert.Equal(t, c.expected, got, "ttl_ms=%d", c.ttlMs)
	}
}

func TestTTLSecondsClampsToMaxInt32(t *testing.T) {
	t.Parallel()
	now := int64(0)
	// Larger than MaxInt32 seconds in the future.
	farFuture := int64(maxInt32)*1000 + 1_000_000
	got, ok := ttlSecondsUntil(farFuture, now)
	require.True(t, ok)
	assert.Equal(t, int32(maxInt32), got)
}

func TestSessionStoreCQLStatementsReferenceKeyspace(t *testing.T) {
	t.Parallel()
	s := &SessionStore{keyspace: "auth_runtime"}
	assert.Contains(t, s.cqlInsertSession(), "auth_runtime.sessions_by_id")
	assert.Contains(t, s.cqlInsertSession(), "USING TTL ?")
	assert.Contains(t, s.cqlSelectSession(), "WHERE tenant = ? AND session_id = ?")
	assert.Contains(t, s.cqlDeleteSession(), "DELETE FROM auth_runtime.sessions_by_id")
}

func TestSessionStoreCustomKeyspace(t *testing.T) {
	t.Parallel()
	s := &SessionStore{keyspace: "custom_auth"}
	assert.Contains(t, s.cqlInsertSession(), "custom_auth.sessions_by_id")
	assert.Contains(t, s.cqlSelectSession(), "FROM custom_auth.sessions_by_id")
}

func TestPutEmptySessionIDRejected(t *testing.T) {
	t.Parallel()
	// Validate that the input check fires before any DB call.
	s := &SessionStore{keyspace: "auth_runtime"}
	err := s.Put(nil, repos.Session{ID: "   ", Tenant: "t"})
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err))
	assert.Contains(t, err.Error(), "session id must not be empty")
}
