package domain

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

func TestSynthesizePersonRecordsHonoursMinCount(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultResolutionJobConfig()
	cfg.RecordCount = 3 // < 9 → bumped to 9 in build_records.
	got := SynthesizeEntityRecords("person", cfg)
	if len(got) != 9 {
		t.Fatalf("expected 9 records, got %d", len(got))
	}
}

func TestSynthesizeOrganizationDispatch(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultResolutionJobConfig()
	got := SynthesizeEntityRecords("organization", cfg)
	if len(got) != 12 {
		t.Fatalf("default record_count=12, got %d", len(got))
	}
	for _, r := range got {
		// Organization profiles always reference an organization-shaped name
		// + organization-shaped record id prefix.
		if !contains(r.RecordID, ":organization:") {
			t.Fatalf("unexpected record id: %s", r.RecordID)
		}
	}
}

func TestSynthesizeRespectsCustomSourceLabels(t *testing.T) {
	t.Parallel()
	cfg := models.ResolutionJobConfig{
		SourceLabels: []string{"custom", "another"},
		RecordCount:  4, // bumped to 9 internally.
	}
	got := SynthesizeEntityRecords("person", cfg)
	for _, r := range got {
		if r.Source != "custom" && r.Source != "another" {
			t.Fatalf("unexpected source %s", r.Source)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
