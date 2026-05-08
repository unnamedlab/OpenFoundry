package statemachine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Error helpers ------------------------------------------------------

func TestTransitionErrorWraps(t *testing.T) {
	t.Parallel()
	err := InvalidTransition("approve in pending state")
	assert.True(t, IsTransitionError(err))
	assert.False(t, IsStale(err))
	assert.Contains(t, err.Error(), "invalid transition: approve in pending state")
}

func TestStaleErrorIdentified(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	err := &StaleError{ID: id, ExpectedVersion: 7}
	assert.True(t, IsStale(err))
	assert.False(t, IsNotFound(err))
	assert.Contains(t, err.Error(), "stale write")
	assert.Contains(t, err.Error(), id.String())
	assert.Contains(t, err.Error(), "expected version=7")
}

func TestNotFoundErrorIdentified(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	err := &NotFoundError{ID: id}
	assert.True(t, IsNotFound(err))
	assert.False(t, IsTransitionError(err))
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), id.String())
}

func TestErrorsAreWrappable(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("outer: " + (&StaleError{ID: uuid.New(), ExpectedVersion: 1}).Error())
	// IsStale unwraps via errors.As.
	stale := &StaleError{ID: uuid.New(), ExpectedVersion: 2}
	wrapped2 := errors.Join(errors.New("network"), stale)
	assert.True(t, IsStale(wrapped2))
	assert.False(t, IsStale(wrapped))
}

// --- WithRetry -----------------------------------------------------------

func TestWithRetrySucceedsFirstAttempt(t *testing.T) {
	t.Parallel()
	got, err := WithRetry(context.Background(), 3, 1*time.Millisecond,
		func(attempt uint32) (string, error) {
			assert.Equal(t, uint32(1), attempt)
			return "ok", nil
		})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

func TestWithRetryReturnsImmediatelyOnNonStale(t *testing.T) {
	t.Parallel()
	calls := 0
	want := errors.New("non-stale boom")
	_, err := WithRetry(context.Background(), 3, 1*time.Millisecond,
		func(attempt uint32) (string, error) {
			calls++
			return "", want
		})
	assert.Equal(t, want, err)
	assert.Equal(t, 1, calls, "non-stale errors must NOT retry")
}

func TestWithRetryRetriesStaleAndStops(t *testing.T) {
	t.Parallel()
	calls := 0
	id := uuid.New()
	got, err := WithRetry(context.Background(), 3, 1*time.Millisecond,
		func(attempt uint32) (string, error) {
			calls++
			if attempt < 3 {
				return "", &StaleError{ID: id, ExpectedVersion: int64(attempt)}
			}
			return "settled", nil
		})
	require.NoError(t, err)
	assert.Equal(t, "settled", got)
	assert.Equal(t, 3, calls)
}

func TestWithRetryExhaustsOnPersistentStale(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	_, err := WithRetry(context.Background(), 2, 1*time.Millisecond,
		func(attempt uint32) (string, error) {
			return "", &StaleError{ID: id, ExpectedVersion: int64(attempt)}
		})
	require.Error(t, err)
	assert.True(t, IsStale(err), "must surface the underlying stale on exhaustion")
}

func TestWithRetryRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	id := uuid.New()
	cancel() // cancelled before first sleep
	_, err := WithRetry(ctx, 5, 50*time.Millisecond,
		func(attempt uint32) (string, error) {
			if attempt == 1 {
				return "", &StaleError{ID: id, ExpectedVersion: 1}
			}
			return "should-not-reach", nil
		})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWithRetryZeroAttemptsRejected(t *testing.T) {
	t.Parallel()
	_, err := WithRetry(context.Background(), 0, 1*time.Millisecond,
		func(uint32) (string, error) { return "x", nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maxAttempts must be > 0")
}

// --- Aggregate contract example -----------------------------------------

// approval is the canonical example aggregate from the Rust doc-test.
type approval struct {
	ID    uuid.UUID `json:"id"`
	State string    `json:"state"`
}

type approvalEvent string

const (
	evtSubmit  approvalEvent = "submit"
	evtApprove approvalEvent = "approve"
)

func (a *approval) Apply(e approvalEvent) error {
	switch a.State {
	case "pending":
		if e == evtSubmit {
			a.State = "awaiting_approval"
			return nil
		}
	case "awaiting_approval":
		if e == evtApprove {
			a.State = "approved"
			return nil
		}
	}
	return InvalidTransition(string(e) + " not valid in state " + a.State)
}

func (a *approval) CurrentState() string  { return a.State }
func (a *approval) AggregateID() uuid.UUID { return a.ID }
func (a *approval) ExpiresAt() *time.Time  { return nil }

func TestAggregateContractCompiles(t *testing.T) {
	t.Parallel()
	// Compile-time enforcement that `*approval` satisfies
	// Aggregate[approvalEvent].
	var _ Aggregate[approvalEvent] = (*approval)(nil)

	a := &approval{ID: uuid.New(), State: "pending"}
	require.NoError(t, a.Apply(evtSubmit))
	assert.Equal(t, "awaiting_approval", a.CurrentState())
	require.NoError(t, a.Apply(evtApprove))
	assert.Equal(t, "approved", a.CurrentState())

	require.Error(t, a.Apply(evtSubmit))
	require.True(t, IsTransitionError(a.Apply(evtSubmit)))
}

func TestAggregateRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	a := &approval{ID: uuid.New(), State: "approved"}
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	restored := &approval{}
	require.NoError(t, json.Unmarshal(raw, restored))
	assert.Equal(t, a.ID, restored.ID)
	assert.Equal(t, a.State, restored.State)
}
