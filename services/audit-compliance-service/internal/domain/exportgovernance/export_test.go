package exportgovernance

import "testing"

func TestEvaluateExportRequiresJustificationAndBlocksPolicyViolations(t *testing.T) {
	decision := EvaluateExport(ExportPolicy{
		CheckpointRequiredKinds:    []ExportKind{ExportCSV, ExportDataset},
		JustificationRequiredKinds: []ExportKind{ExportCSV},
		BlockedMarkings:            []string{"pii"},
		AllowedDestinations:        []string{"managed-download"},
	}, ExportRequest{
		Kind:                     ExportCSV,
		ResourceRID:              "dataset-1",
		Filters:                  map[string]string{"country": "US"},
		Parameters:               map[string]string{"format": "csv"},
		Branch:                   "main",
		Markings:                 []string{"pii"},
		RestrictedViewPolicyID:   "rv-1",
		UserID:                   "user-1",
		Destination:              "external-s3",
		MarkingAllowed:           false,
		RestrictedViewAllowed:    true,
		ObjectSecurityAllowed:    true,
		ApplicationPolicyAllowed: true,
		EgressPolicyAllowed:      false,
	})
	if decision.Allowed {
		t.Fatalf("expected blocked export: %#v", decision)
	}
	if !decision.CheckpointRequired || !decision.JustificationRequired {
		t.Fatalf("expected checkpoint and justification: %#v", decision)
	}
	for _, want := range []string{"export justification is required", "export blocked by marking pii", "destination is not allowed by export policy", "export violates markings", "export violates egress policy"} {
		if !containsFold(decision.BlockingReasons, want) {
			t.Fatalf("blocking reasons %#v missing %q", decision.BlockingReasons, want)
		}
	}
	if decision.Provenance.Filters["country"] != "US" || decision.Provenance.RestrictedViewPolicyID != "rv-1" {
		t.Fatalf("provenance missing fields: %#v", decision.Provenance)
	}
}

func TestEvaluateExportAllowsCompleteProvenance(t *testing.T) {
	decision := EvaluateExport(ExportPolicy{JustificationRequiredKinds: []ExportKind{ExportDashboard}}, ExportRequest{
		Kind: ExportDashboard, ResourceRID: "dash-1", UserID: "user-1", Destination: "managed-download", Justification: "board review",
		MarkingAllowed: true, RestrictedViewAllowed: true, ObjectSecurityAllowed: true, ApplicationPolicyAllowed: true, EgressPolicyAllowed: true,
	})
	if !decision.Allowed {
		t.Fatalf("expected allowed: %#v", decision)
	}
	if decision.Provenance.Justification != "board review" {
		t.Fatalf("missing justification provenance: %#v", decision.Provenance)
	}
}
