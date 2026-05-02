package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/open-foundry/open-foundry/workers-go/approvals/activities"
	"github.com/open-foundry/open-foundry/workers-go/approvals/internal/contract"
)

// ApprovalRequestWorkflow is the canonical S2.5 substrate. State
// (request, approver_set, decision) lives here as durable workflow
// state — Cassandra/Postgres are no longer authoritative for
// approvals once S2.5.d drops the legacy tables.
//
// Pattern:
//   - Workflow blocks on a `decide` signal.
//   - Optional timeout (24 h default) → expired.
//   - On decision, an activity (S2.5.c) emits the audit event to
//     Kafka `audit.events` via `audit-compliance-service` gRPC.
func ApprovalRequestWorkflow(
	ctx workflow.Context,
	input contract.ApprovalRequestInput,
) (contract.ApprovalResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("ApprovalRequest started",
		"request_id", input.RequestID,
		"approver_set", input.ApproverSet,
	)

	var decision contract.ApprovalDecision
	signalCh := workflow.GetSignalChannel(ctx, contract.SignalDecide)

	// 24 h hard timeout. The exact value should come from policy in
	// a follow-up PR (S2.5.b allows per-tenant overrides).
	timeoutCtx, cancel := workflow.WithCancel(ctx)
	defer cancel()
	timer := workflow.NewTimer(timeoutCtx, 24*time.Hour)

	selector := workflow.NewSelector(ctx)
	selector.AddReceive(signalCh, func(ch workflow.ReceiveChannel, _ bool) {
		ch.Receive(ctx, &decision)
	})
	selector.AddFuture(timer, func(_ workflow.Future) {
		decision = contract.ApprovalDecision{Outcome: "expired"}
	})
	selector.Select(ctx)

	result := contract.ApprovalResult{RequestID: input.RequestID}
	switch decision.Outcome {
	case "approve":
		result.Decision = "approved"
		result.Approver = decision.Approver
	case "reject":
		result.Decision = "rejected"
		result.Approver = decision.Approver
	default:
		result.Decision = "expired"
	}

	// S2.5.c — emit audit event after the decision is durable.
	// Activity options match the audit-trail SLO (≤30s, ≤3 attempts).
	auditOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
		},
	}
	auditCtx := workflow.WithActivityOptions(ctx, auditOpts)
	evt := activities.AuditEvent{
		OccurredAt:         workflow.Now(ctx),
		TenantID:           input.TenantID,
		Actor:              result.Approver,
		Action:             "approval." + result.Decision,
		ResourceType:       "approval_request",
		ResourceID:         input.RequestID,
		AuditCorrelationID: workflow.GetInfo(ctx).WorkflowExecution.ID,
		Attributes: map[string]any{
			"subject":      input.Subject,
			"approver_set": input.ApproverSet,
		},
	}
	if err := workflow.ExecuteActivity(auditCtx, (*activities.Activities).EmitAuditEvent, evt).
		Get(ctx, nil); err != nil {
		// Never fail the workflow on audit-emit error: the decision
		// is already durable in Temporal history and a downstream
		// reconciler can replay missing audit events.
		logger.Warn("audit emit failed",
			"err", err,
			"request_id", input.RequestID,
		)
	}

	return result, nil
}
