package cedarauthz

import (
	"context"
	"log/slog"
	"time"
)

// AuthzAuditEvent is the wire format for the `audit.authz.v1` Kafka
// topic. Field set + JSON tags match the Rust struct byte-for-byte so
// downstream consumers (audit-trail, audit-compliance) read events from
// either runtime indistinguishably.
type AuthzAuditEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Principal   string    `json:"principal"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource"`
	Decision    string    `json:"decision"`
	Tenant      *string   `json:"tenant,omitempty"`
	PolicyIDs   []string  `json:"policy_ids,omitempty"`
	Diagnostics []string  `json:"diagnostics,omitempty"`
}

// AuthzAuditSink consumes one event at a time. Implementations MUST be
// safe for concurrent use — the engine emits from a goroutine, so
// multiple Emits may overlap.
//
// The contract is fire-and-forget: a slow sink must NEVER stall the
// request hot path. Errors are intentionally silent; sinks that need to
// surface failures should log them internally.
type AuthzAuditSink interface {
	Emit(ctx context.Context, event AuthzAuditEvent)
}

// NoopAuditSink drops every event. Default for tests and the in-memory
// engine.
type NoopAuditSink struct{}

func (NoopAuditSink) Emit(context.Context, AuthzAuditEvent) {}

// SlogAuditSink logs every decision at INFO level via the supplied
// logger (or the default logger when nil). Useful in dev / smoke runs.
//
// Replaces the Rust `TracingAuditSink` — same emission semantics, just
// using log/slog instead of the tracing crate.
type SlogAuditSink struct {
	Logger *slog.Logger
}

func (s SlogAuditSink) Emit(_ context.Context, event AuthzAuditEvent) {
	log := s.Logger
	if log == nil {
		log = slog.Default()
	}
	attrs := []any{
		slog.String("principal", event.Principal),
		slog.String("action", event.Action),
		slog.String("resource", event.Resource),
		slog.String("decision", event.Decision),
		slog.Any("policies", event.PolicyIDs),
	}
	if event.Tenant != nil {
		attrs = append(attrs, slog.String("tenant", *event.Tenant))
	}
	log.Info("authz decision", attrs...)
}
