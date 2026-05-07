package cedarauthz

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/nats-io/nats.go"
)

// DefaultReloadSubject is the NATS subject the reload subscriber listens on.
const DefaultReloadSubject = "authz.policy.changed"

// ReloadFunc is invoked once per inbound NATS message. The message
// payload is intentionally ignored — it's a signal-only contract — so
// publishers can send anything (typically empty body or `{"version":N}`).
type ReloadFunc func(ctx context.Context) (int, error)

// PolicyReloadSubscriber listens on `authz.policy.changed` and re-pulls
// the policy bundle every time a message arrives.
//
// Lifecycle: build with [NewReloadSubscriber], optionally call
// [WithSubject], then [Run] to spawn the background subscription. Drop
// the returned [*PolicyReloadHandle] to stop.
type PolicyReloadSubscriber struct {
	nc      *nats.Conn
	subject string
}

// NewReloadSubscriber wires the subscriber against an existing NATS
// connection. The connection is NOT owned by the subscriber — callers
// stay responsible for nc.Close().
func NewReloadSubscriber(nc *nats.Conn) *PolicyReloadSubscriber {
	return &PolicyReloadSubscriber{nc: nc, subject: DefaultReloadSubject}
}

// WithSubject overrides the subject (defaults to `authz.policy.changed`).
func (s *PolicyReloadSubscriber) WithSubject(subject string) *PolicyReloadSubscriber {
	s.subject = subject
	return s
}

// Run subscribes to the reload subject and invokes `reload` once per
// inbound message. Failures are logged but never stop the subscription
// — a transient Postgres error must not silently disable hot-reload.
//
// Returns a handle that the caller drops (or calls Shutdown on) to
// terminate the subscription.
func (s *PolicyReloadSubscriber) Run(ctx context.Context, reload ReloadFunc) (*PolicyReloadHandle, error) {
	if reload == nil {
		return nil, errors.New("cedarauthz: reload func is nil")
	}
	handle := &PolicyReloadHandle{}
	sub, err := s.nc.Subscribe(s.subject, func(msg *nats.Msg) {
		if handle.stopped.Load() {
			return
		}
		slog.Debug("cedar policy reload signal received",
			slog.String("subject", msg.Subject),
			slog.Int("bytes", len(msg.Data)),
		)
		count, err := reload(ctx)
		if err != nil {
			slog.Error("cedar policy reload failed; keeping previous bundle",
				slog.String("error", err.Error()),
			)
			return
		}
		slog.Info("cedar policies reloaded", slog.Int("policies", count))
	})
	if err != nil {
		return nil, err
	}
	handle.sub = sub
	slog.Info("cedar policy reload subscriber started",
		slog.String("subject", s.subject),
	)
	return handle, nil
}

// PolicyReloadHandle owns the active subscription. Calling Shutdown is
// idempotent — repeated calls are a no-op.
type PolicyReloadHandle struct {
	sub     *nats.Subscription
	stopped atomic.Bool
}

// Shutdown terminates the subscription. Safe to call multiple times.
func (h *PolicyReloadHandle) Shutdown() error {
	if h == nil || !h.stopped.CompareAndSwap(false, true) {
		return nil
	}
	if h.sub == nil {
		return nil
	}
	return h.sub.Unsubscribe()
}
