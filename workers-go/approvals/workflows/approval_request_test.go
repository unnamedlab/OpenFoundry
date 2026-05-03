package workflows

import (
	"context"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"github.com/open-foundry/open-foundry/workers-go/approvals/activities"
	"github.com/open-foundry/open-foundry/workers-go/approvals/internal/contract"
)

type workflowAuditPublisher struct {
	events []activities.AuditEvent
}

func (p *workflowAuditPublisher) AppendEvent(_ context.Context, evt activities.AuditEvent) (map[string]any, error) {
	p.events = append(p.events, evt)
	return map[string]any{"id": "audit-1"}, nil
}

func TestApprovalWorkflowRecordsDecisionAndEmitsAudit(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	publisher := &workflowAuditPublisher{}
	env.RegisterWorkflow(ApprovalRequestWorkflow)
	env.RegisterActivity(activities.NewWithAuditClient(publisher))
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(contract.SignalDecide, contract.ApprovalDecision{
			Outcome:  "approve",
			Approver: "approver@example.com",
			Comment:  "looks good",
		})
	}, 0)

	env.ExecuteWorkflow(ApprovalRequestWorkflow, contract.ApprovalRequestInput{
		RequestID:   "approval-1",
		TenantID:    "tenant-a",
		Subject:     "deploy to prod",
		ApproverSet: []string{"approver@example.com"},
		ActionPayload: map[string]any{
			"change_id": "chg-1",
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result contract.ApprovalResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Decision != "approved" || result.Approver != "approver@example.com" {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("audit events len = %d", len(publisher.events))
	}
	if publisher.events[0].Action != "approval.approved" {
		t.Fatalf("audit action = %q", publisher.events[0].Action)
	}
}
