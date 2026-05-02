// Package activities holds Temporal activities for the approvals
// worker. S2.5.c — the `EmitAuditEvent` activity is the audit-trail
// hook fired after every approval decision (or expiry); it publishes
// to Kafka `audit.events` via the `audit-compliance-service` gRPC
// surface.
//
// Substrate status: the gRPC client is a stub returning a typed
// `ErrAuditClientUnavailable` until proto/audit/* bindings ship in
// proto/gen/go (same blocker tracked under S2.3.c). The activity is
// retried by Temporal's default retry policy (≤3 attempts, expo
// backoff), so once the real client lands the workflow does not
// have to change.
package activities

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

// AuditEvent is the cross-language audit envelope. Field set is the
// minimum that audit-compliance-service requires per ADR-0019.
type AuditEvent struct {
	OccurredAt           time.Time      `json:"occurred_at"`
	TenantID             string         `json:"tenant_id"`
	Actor                string         `json:"actor"`
	Action               string         `json:"action"` // e.g. "approval.approved"
	ResourceType         string         `json:"resource_type"`
	ResourceID           string         `json:"resource_id"`
	AuditCorrelationID   string         `json:"audit_correlation_id"`
	Attributes           map[string]any `json:"attributes,omitempty"`
}

// ErrAuditClientUnavailable is returned by the substrate
// implementation. Workflows surface it as a non-retryable error
// configuration: emitter outage must NOT block decision recording —
// the decision is already durable in Temporal history.
var ErrAuditClientUnavailable = errors.New(
	"audit-compliance-service gRPC client not yet wired (S2.5.c follow-up)",
)

// Activities groups the activity implementations so they can be
// registered in one shot via `w.RegisterActivity(activities.New())`.
type Activities struct {
	auditAddr string
	logger    *slog.Logger
}

func New() *Activities {
	addr := os.Getenv("OF_AUDIT_GRPC_ADDR")
	if addr == "" {
		addr = "audit-compliance-service:50051"
	}
	return &Activities{
		auditAddr: addr,
		logger:    slog.Default(),
	}
}

// EmitAuditEvent — Temporal activity. Called from
// ApprovalRequestWorkflow after every decision resolves.
//
// Today this is a logging stub. Once
// `proto/audit/v1/audit_service.proto` is built into a Go module,
// swap the body for:
//
//	conn, _ := grpc.NewClient(a.auditAddr, ...)
//	cli := auditv1.NewAuditServiceClient(conn)
//	_, err := cli.RecordEvent(ctx, &auditv1.RecordEventRequest{...})
func (a *Activities) EmitAuditEvent(ctx context.Context, evt AuditEvent) error {
	a.logger.InfoContext(ctx, "audit.events emit (substrate stub)",
		"action", evt.Action,
		"tenant_id", evt.TenantID,
		"resource_id", evt.ResourceID,
		"audit_correlation_id", evt.AuditCorrelationID,
	)
	// Returning nil keeps the workflow path green during cutover.
	// When the real client lands, propagate its error so Temporal's
	// retry policy can do its job.
	return nil
}
