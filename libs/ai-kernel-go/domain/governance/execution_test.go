package governance

import "testing"

func TestEvaluateAIExecutionBlocksPrivilegeEscalationAndUnapprovedMutation(t *testing.T) {
	decision := EvaluateAIExecution(AIExecutionRequest{
		InvokingUserID:          "user-a",
		EffectiveIdentityType:   ExecutionIdentityUser,
		EffectiveSubjectID:      "user-b",
		Mutating:                true,
		AISessionLogEnabled:     true,
		StandardAuditLogEnabled: true,
	})
	if decision.Allowed {
		t.Fatalf("expected blocked decision: %#v", decision)
	}
	for _, want := range []string{"AI user-scoped execution cannot change the effective user identity", "mutating AI operation requires explicit user approval"} {
		if !containsFold(decision.BlockingReasons, want) {
			t.Fatalf("blocking reasons %#v missing %q", decision.BlockingReasons, want)
		}
	}
}

func TestEvaluateAIExecutionAllowsConfiguredServiceIdentityAndAttributesUsage(t *testing.T) {
	decision := EvaluateAIExecution(AIExecutionRequest{
		InvokingUserID:             "user-a",
		ConfiguredServiceAccountID: "svc-ai-approved",
		EffectiveIdentityType:      ExecutionIdentityService,
		EffectiveSubjectID:         "svc-ai-approved",
		Mutating:                   true,
		UserApproved:               true,
		AISessionLogEnabled:        true,
		StandardAuditLogEnabled:    true,
	})
	if !decision.Allowed {
		t.Fatalf("expected allowed: %#v", decision)
	}
	if decision.LLMAttributionID != "svc-ai-approved" || decision.RateLimitSubjectID != "svc-ai-approved" {
		t.Fatalf("unexpected attribution: %#v", decision)
	}
}
