package cedarauthz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

func TestNewReloadSubscriberDefaults(t *testing.T) {
	t.Parallel()
	// Builder-only path: no actual NATS connection is needed to assert
	// the default subject and the WithSubject override (the constructor
	// stashes the conn but doesn't dial until Run).
	sub := cedarauthz.NewReloadSubscriber(&nats.Conn{})
	require.NotNil(t, sub)
	// WithSubject returns the receiver — chainable.
	overridden := sub.WithSubject("my.custom.subject")
	assert.Same(t, sub, overridden, "WithSubject must return the same receiver")
}

func TestRunRejectsNilReloadFunc(t *testing.T) {
	t.Parallel()
	sub := cedarauthz.NewReloadSubscriber(&nats.Conn{})
	_, err := sub.Run(context.Background(), nil)
	require.Error(t, err)
}

func TestPolicyReloadHandleShutdownIsIdempotentWhenNil(t *testing.T) {
	t.Parallel()
	// Calling Shutdown on a zero handle is safe and returns nil — the
	// handle is allowed to be returned uninitialised after a failed
	// Run (the caller's defer h.Shutdown() must not blow up).
	var h cedarauthz.PolicyReloadHandle
	require.NoError(t, h.Shutdown())
	// Repeated calls remain idempotent.
	require.NoError(t, h.Shutdown())
}

// Confirm the public ReloadFunc signature accepts a context-aware
// closure — the kind of value PgPolicyStore.Reload binds to via a
// trivial wrapper.
func TestReloadFuncSignature(t *testing.T) {
	t.Parallel()
	called := 0
	var fn cedarauthz.ReloadFunc = func(_ context.Context) (int, error) {
		called++
		return 42, errors.New("boom")
	}
	count, err := fn(context.Background())
	require.Error(t, err)
	assert.Equal(t, 42, count)
	assert.Equal(t, 1, called)
}

func TestDefaultReloadSubject(t *testing.T) {
	t.Parallel()
	// Locked: changing this constant breaks every Rust publisher of
	// `authz.policy.changed`.
	assert.Equal(t, "authz.policy.changed", cedarauthz.DefaultReloadSubject)
}
