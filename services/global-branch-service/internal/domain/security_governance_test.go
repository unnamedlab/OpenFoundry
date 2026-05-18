package domain

import "testing"

func TestPlanBranchRestrictedViewWarnsOnMarkingDiffAndRequiresPermissions(t *testing.T) {
	plan := PlanBranchRestrictedView(BranchRestrictedViewPolicyChange{
		RestrictedViewID:      "rv-1",
		ObjectTypeID:          "object-type-1",
		PolicyJSON:            `{"rule":"branch"}`,
		MainPolicyJSON:        `{"rule":"main"}`,
		BranchMarkingIDs:      []string{"public", "pii"},
		MainMarkingIDs:        []string{"public"},
		ViewPermission:        true,
		BuildRequested:        true,
		PropagateToObjectType: true,
	})

	if !plan.PolicyChanged || !plan.UpstreamMarkingsDiff || !plan.RequiresBuild || !plan.RequiresPropagation {
		t.Fatalf("unexpected plan flags: %#v", plan)
	}
	if len(plan.Warnings) != 1 || plan.Warnings[0] != BranchRestrictedViewMarkingWarning {
		t.Fatalf("warning=%#v", plan.Warnings)
	}
	wantReasons := []string{
		"edit restricted view policy permission required",
		"approve branch restricted view policy permission required",
		"merge branch security-resource permission required",
	}
	for _, want := range wantReasons {
		if !contains(plan.BlockingReasons, want) {
			t.Fatalf("blocking reasons %#v missing %q", plan.BlockingReasons, want)
		}
	}
}

func TestBuildSecurityDiffReportRequiresApprovalAndBlocksRuntimeLeak(t *testing.T) {
	report := BuildSecurityDiffReport([]SecurityDiffChange{
		{Kind: SecurityDiffRole, ResourceID: "project-viewer", ExpandsAccess: true, BranchOnly: true},
		{Kind: SecurityDiffRestrictedViewPolicy, ResourceID: "rv-1", ReducesControl: true},
		{Kind: SecurityDiffMarking, ResourceID: "export", Before: "required", After: "removed", ReducesControl: true},
	})

	if !report.RequiresApproval {
		t.Fatalf("expected approval: %#v", report)
	}
	if !report.PreventMainlineRuntimeLeak {
		t.Fatalf("expected runtime leak prevention: %#v", report)
	}
	for _, want := range []string{
		"role:project-viewer expands access",
		"restricted_view_policy:rv-1 reduces security controls",
		"marking:export reduces security controls",
	} {
		if !contains(report.ApprovalReasons, want) {
			t.Fatalf("approval reasons %#v missing %q", report.ApprovalReasons, want)
		}
	}
	if len(report.Warnings) != 1 {
		t.Fatalf("warnings=%#v", report.Warnings)
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
