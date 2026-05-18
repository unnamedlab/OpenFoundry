package workspace

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrashPurgeModeRequiresRetentionOrAdmin(t *testing.T) {
	t.Parallel()
	purgeAfter := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	snapshot := trashPurgeSnapshot{PurgeAfter: &purgeAfter}

	mode, err := snapshot.purgeMode(time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC), false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTrashRetentionActive))
	assert.Empty(t, mode)

	mode, err = snapshot.purgeMode(time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC), true)
	require.NoError(t, err)
	assert.Equal(t, "admin_override", mode)

	mode, err = snapshot.purgeMode(time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC), false)
	require.NoError(t, err)
	assert.Equal(t, "retention_expired", mode)
}
