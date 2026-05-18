package governanceops

import (
	"testing"
	"time"
)

func TestFindingCaseLifecycleLinksEvidenceAndRequiresClosedTasksForApproval(t *testing.T) {
	now := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	f := NewFinding(FindingInput{Source: FindingRiskyEgress, Severity: "high", Title: "Risky egress", AuditEventIDs: []string{"audit-1"}, ResourceRIDs: []string{"egress-1"}, ActorIDs: []string{"user-1"}, PolicyDecisionIDs: []string{"deny-1"}, DetectedAt: now})
	f = AssignFinding(f, "analyst@example.com", now)
	f = AddFindingComment(f, "analyst@example.com", "investigating", now)
	f = AddRemediationTask(f, "disable policy", "owner@example.com")
	f = AddEvidenceLink(f, "audit timeline", "https://evidence.local/audit-1", "audit")
	f = ApproveFindingClosure(f, "security@example.com", "approved", "done", now)
	if f.Status == "closed" {
		t.Fatalf("finding should stay open until tasks are closed: %#v", f)
	}
	f.Tasks[0].Status = "done"
	f = ApproveFindingClosure(f, "security@example.com", "approved", "done", now)
	if f.Status != "closed" {
		t.Fatalf("expected closed finding: %#v", f)
	}
	if len(f.AuditEventIDs) != 1 || len(f.EvidenceLinks) != 1 || len(f.ClosureApprovals) != 2 {
		t.Fatalf("links missing: %#v", f)
	}
}

func TestAccessReviewRequiresItemDecisionAndBuildsImpactGraph(t *testing.T) {
	review := ScheduleAccessReview("Quarterly", 30, time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), []AccessReviewItem{
		{ID: "item-1", Type: EntitlementProjectRole, PrincipalID: "group:analysts", ResourceID: "project-1", Role: "owner", Decision: "attested"},
		{ID: "item-2", Type: EntitlementMarkingMembership, PrincipalID: "user-1", ResourceID: "marking:pii"},
	})
	if len(review.EffectiveAccessGraph.Edges) != 2 || len(review.GroupProjectImpact) != 1 {
		t.Fatalf("expected graph and impact: %#v", review)
	}
	review = CloseAccessReview(review)
	if review.Status == "closed" || !containsString(review.BlockingReasons, "item-2 requires attestation, removal, or exception") {
		t.Fatalf("expected blocker: %#v", review)
	}
	review.Items[1].Decision = "exception"
	review.Items[1].ExceptionReason = "break-glass owner"
	review = CloseAccessReview(review)
	if review.Status != "closed" {
		t.Fatalf("expected closed: %#v", review)
	}
}

func TestLeastPrivilegeRecommendationsProduceApprovalProposalsAndFindings(t *testing.T) {
	proposals := RecommendLeastPrivilege(LeastPrivilegeSignals{OwnerGrants: []string{"user:owner"}, StaleTokenIDs: []string{"token-1"}, UnscopedOAuthAppIDs: []string{"oauth-1"}, UnredactedEmailChannels: []string{"email-default"}})
	if len(proposals) != 4 {
		t.Fatalf("proposals=%#v", proposals)
	}
	for _, p := range proposals {
		if !p.ApprovalRequired || p.SafeAction == "" {
			t.Fatalf("unsafe proposal: %#v", p)
		}
	}
	finding := NewFinding(ProposalFinding(proposals[0]))
	if finding.Source != FindingPermissionDrift || len(finding.ResourceRIDs) == 0 {
		t.Fatalf("finding=%#v", finding)
	}
}

func TestSelfHostedChecklistCoversRequiredCategoriesAndAuditIntegration(t *testing.T) {
	checklist := BuildSelfHostedChecklist("prod-us")
	if checklist.Warning != SelfHostedResponsibilityWarning {
		t.Fatalf("warning=%q", checklist.Warning)
	}
	for _, category := range []string{"host", "network", "audit", "patch", "certificate", "backup", "monitoring"} {
		found := false
		for _, item := range checklist.Items {
			if item.Category == category && item.AuditIntegration != "" && len(item.LogSources) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("category %s missing from %#v", category, checklist.Items)
		}
	}
}

func TestUsageBudgetsCreateSecurityFindingsForAnomalies(t *testing.T) {
	anomalies := EvaluateUsageBudgets([]UsageSample{{UsageType: "query", Quantity: 120}, {UsageType: "query", Quantity: 30}}, []UsageBudget{{ID: "budget-query", UsageType: "query", Limit: 100, FindingSeverity: "high"}})
	if len(anomalies) != 1 || anomalies[0].Finding == nil {
		t.Fatalf("anomalies=%#v", anomalies)
	}
	if anomalies[0].Finding.Severity != "high" || !containsString(anomalies[0].Finding.PolicyDecisionIDs, "budget-query") {
		t.Fatalf("finding=%#v", anomalies[0].Finding)
	}
}

func TestPolicyAsCodeDryRunDiffDriftAndPermissionChecks(t *testing.T) {
	decision := EvaluatePolicyAsCode(PolicyAsCodePlan{Version: "v1", DryRun: true, AuditEnabled: true, ActorPermissions: []string{"roles:write"}, Resources: []DeclarativePolicyResource{
		{Kind: PolicyRole, Name: "analyst", DesiredHash: "a", LiveHash: "b", RequiresPermission: "roles:write"},
		{Kind: PolicyRestrictedView, Name: "rv", DesiredHash: "c", RequiresPermission: "restricted_views:write"},
	}})
	if decision.CanRollout || len(decision.Diffs) != 2 || len(decision.Drift) != 1 {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if !containsString(decision.BlockingReasons, "missing permission restricted_views:write for rv") {
		t.Fatalf("expected missing permission: %#v", decision.BlockingReasons)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
