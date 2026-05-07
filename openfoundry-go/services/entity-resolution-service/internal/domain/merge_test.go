package domain

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

func sampleCluster() models.ResolvedCluster {
	return models.ResolvedCluster{
		ID:              uuid.New(),
		ClusterKey:      "k",
		Status:          "resolved",
		ConfidenceScore: 0.91,
		Records: []models.EntityRecord{
			{
				RecordID: "crm:person:1", Source: "crm", ExternalID: "crm-1",
				DisplayName: "John Smith", Confidence: 0.92,
				Attributes: map[string]any{"email": "john.smith@acme.com", "phone": "+1 415 555 0100", "company": "Acme Logistics"},
			},
			{
				RecordID: "erp:person:1", Source: "erp", ExternalID: "erp-1",
				DisplayName: "Jon Smyth", Confidence: 0.85,
				Attributes: map[string]any{"email": "jon.smyth@acme.com", "phone": "+1 (415) 555-0100", "company": "Acme Logistics"},
			},
		},
	}
}

func TestSynthesizeGoldenRecordDefaultRules(t *testing.T) {
	t.Parallel()
	gr := SynthesizeGoldenRecord(sampleCluster(), models.MergeStrategy{})
	if gr.Title == "" || gr.Title == "Golden Record" {
		t.Fatalf("title should fall back to display_name, got %q", gr.Title)
	}
	if len(gr.Provenance) == 0 {
		t.Fatal("provenance must contain at least one entry")
	}
	if _, ok := gr.CanonicalValues["display_name"]; !ok {
		t.Fatal("display_name must be set")
	}
	if gr.Status != "active" {
		t.Fatalf("status got %s", gr.Status)
	}
	if gr.CompletenessScore <= 0 || gr.CompletenessScore > 1 {
		t.Fatalf("completeness out of range: %v", gr.CompletenessScore)
	}
}

func TestSynthesizeGoldenRecordRespectsRejectedCluster(t *testing.T) {
	t.Parallel()
	c := sampleCluster()
	c.Status = "rejected"
	gr := SynthesizeGoldenRecord(c, models.MergeStrategy{})
	if gr.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", gr.Status)
	}
}

func TestSourcePriorityWinsForEmail(t *testing.T) {
	t.Parallel()
	gr := SynthesizeGoldenRecord(sampleCluster(), models.MergeStrategy{
		Rules: []models.SurvivorshipRule{
			{Field: "email", Strategy: "source_priority", SourcePriority: []string{"erp", "crm"}, Fallback: "longest_non_empty"},
		},
	})
	got, _ := gr.CanonicalValues["email"].(string)
	if got != "jon.smyth@acme.com" {
		t.Fatalf("erp should win priority, got %q", got)
	}
	if gr.Provenance[0].Source != "erp" {
		t.Fatalf("provenance source got %s", gr.Provenance[0].Source)
	}
}
