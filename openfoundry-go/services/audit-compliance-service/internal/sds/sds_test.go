package sds

import (
	"strings"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func TestScanDetectsAndRedacts(t *testing.T) {
	t.Parallel()
	resp := Scan(&models.SensitiveDataScanRequest{
		Content: "Contact jane@example.com with SSN 123-45-6789 and token ofk_abcdefghi",
		Scope:   models.SDSScopeRecord,
	})
	kinds := map[string]bool{}
	for _, f := range resp.Findings {
		kinds[f.Kind] = true
	}
	if !kinds["email"] {
		t.Fatal("expected email detector to fire")
	}
	if !kinds["ssn"] {
		t.Fatal("expected ssn detector to fire")
	}
	if !strings.Contains(resp.RedactedContent, "***-**-****") {
		t.Fatalf("ssn not redacted: %s", resp.RedactedContent)
	}
	if !strings.Contains(resp.RedactedContent, "ofk_[redacted]") {
		t.Fatalf("api_key not redacted: %s", resp.RedactedContent)
	}
	if resp.RiskScore < 50 {
		t.Fatalf("risk_score too low: %d", resp.RiskScore)
	}
}

func TestScanWithRedactFalseLeavesContentUntouched(t *testing.T) {
	t.Parallel()
	redact := false
	resp := Scan(&models.SensitiveDataScanRequest{
		Content: "Contact jane@example.com",
		Redact:  &redact,
	})
	if resp.RedactedContent != "Contact jane@example.com" {
		t.Fatalf("redacted_content mutated: %s", resp.RedactedContent)
	}
	if len(resp.Findings) == 0 {
		t.Fatal("expected at least one finding even when redact=false")
	}
}

func TestScanScopeChangesRiskMultiplier(t *testing.T) {
	t.Parallel()
	content := "Contact jane@example.com"
	prompt := Scan(&models.SensitiveDataScanRequest{Content: content, Scope: models.SDSScopePrompt})
	dataset := Scan(&models.SensitiveDataScanRequest{Content: content, Scope: models.SDSScopeDataset})
	if dataset.RiskScore != 2*prompt.RiskScore {
		t.Fatalf("expected dataset (×2) %d to be 2× prompt (×1) %d", dataset.RiskScore, prompt.RiskScore)
	}
}
