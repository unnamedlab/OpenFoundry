package state

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWireFormatTokens(t *testing.T) {
	t.Parallel()
	cases := map[JobStatus]string{
		StatusQueued: "queued", StatusRunning: "running",
		StatusCompleted: "completed", StatusFailed: "failed", StatusCancelled: "cancelled",
	}
	for s, want := range cases {
		assert.Equal(t, want, s.String(), "wire-format token must match Rust SQL CHECK / completed.v1 status")
	}
}

func TestParseStatusRoundTrips(t *testing.T) {
	t.Parallel()
	for _, s := range AllStatuses {
		got, err := ParseStatus(s.String())
		require.NoError(t, err)
		assert.Equal(t, s, got)
	}
	_, err := ParseStatus("banana")
	require.Error(t, err)
	var unk *UnknownStatusError
	require.True(t, errors.As(err, &unk))
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	assert.False(t, StatusQueued.IsTerminal())
	assert.False(t, StatusRunning.IsTerminal())
	assert.True(t, StatusCompleted.IsTerminal())
	assert.True(t, StatusFailed.IsTerminal())
	assert.True(t, StatusCancelled.IsTerminal())
}

func TestCanTransitionToAllowedMoves(t *testing.T) {
	t.Parallel()
	allowed := []struct{ from, to JobStatus }{
		{StatusQueued, StatusRunning},
		{StatusQueued, StatusCancelled},
		{StatusQueued, StatusFailed},
		{StatusRunning, StatusCompleted},
		{StatusRunning, StatusFailed},
		{StatusRunning, StatusCancelled},
	}
	for _, c := range allowed {
		assert.True(t, c.from.CanTransitionTo(c.to), "%s → %s should be allowed", c.from, c.to)
	}
}

func TestCanTransitionToForbiddenMoves(t *testing.T) {
	t.Parallel()
	forbidden := []struct{ from, to JobStatus }{
		// Terminal → non-terminal is forbidden.
		{StatusCompleted, StatusRunning},
		{StatusCompleted, StatusQueued},
		{StatusFailed, StatusQueued},
		{StatusCancelled, StatusRunning},
		// Backwards moves.
		{StatusRunning, StatusQueued},
	}
	for _, c := range forbidden {
		assert.False(t, c.from.CanTransitionTo(c.to), "%s → %s should NOT be allowed", c.from, c.to)
	}
}

func TestSelfLoopAllowed(t *testing.T) {
	t.Parallel()
	for _, s := range AllStatuses {
		assert.True(t, s.CanTransitionTo(s),
			"idempotent self-loop on %s must be allowed (matches INSERT … ON CONFLICT DO UPDATE writers)", s)
	}
}

func TestEnsureTransitionTypedError(t *testing.T) {
	t.Parallel()
	require.NoError(t, StatusQueued.EnsureTransition(StatusRunning))
	err := StatusCompleted.EnsureTransition(StatusRunning)
	require.Error(t, err)
	var ill *IllegalTransitionError
	require.True(t, errors.As(err, &ill))
	assert.Equal(t, StatusCompleted, ill.From)
	assert.Equal(t, StatusRunning, ill.To)
}
