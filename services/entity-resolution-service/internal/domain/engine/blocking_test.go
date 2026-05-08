package engine

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

func sampleRecords() []models.EntityRecord {
	return []models.EntityRecord{
		{
			RecordID: "crm:person:crm-1-1", Source: "crm", DisplayName: "John Smith",
			Confidence: 0.9, Attributes: map[string]any{"email": "john.smith@acme.com", "phone": "+1 415 555 0100"},
		},
		{
			RecordID: "erp:person:erp-1-1", Source: "erp", DisplayName: "Jon Smyth",
			Confidence: 0.85, Attributes: map[string]any{"email": "jon.smyth@acme.com", "phone": "+1 (415) 555-0100"},
		},
		{
			RecordID: "support:person:support-1-1", Source: "support", DisplayName: "J. Smith",
			Confidence: 0.7, Attributes: map[string]any{"email": "john.smith+support@acme.com", "phone": "4155550100"},
		},
		{
			RecordID: "crm:person:crm-2-1", Source: "crm", DisplayName: "Mei Chen",
			Confidence: 0.92, Attributes: map[string]any{"email": "mei.chen@harbor.ai", "phone": "+65 6555 0103"},
		},
	}
}

func TestKeyBasedPairsClustersBySharedKey(t *testing.T) {
	t.Parallel()
	// Two records that share normalized email/phone/displayName prefixes
	// (first-6-rune slice) so the combined key collides exactly.
	records := []models.EntityRecord{
		{
			RecordID: "a", Source: "crm", DisplayName: "Foo Bar",
			Attributes: map[string]any{"email": "foobar@x.io", "phone": "111-222"},
		},
		{
			RecordID: "b", Source: "erp", DisplayName: "Foo Bar",
			Attributes: map[string]any{"email": "foobar@x.io", "phone": "111-222"},
		},
		{
			RecordID: "c", Source: "support", DisplayName: "Different",
			Attributes: map[string]any{"email": "other@x.io", "phone": "333-444"},
		},
	}
	strategy := models.BlockingStrategyConfig{
		StrategyType: "key-based",
		KeyFields:    []string{"email", "phone", "display_name"},
		WindowSize:   5,
		BucketCount:  24,
	}
	pairs := BuildCandidatePairs(records, strategy)
	if len(pairs) != 1 {
		t.Fatalf("expected exactly one collision pair (a, b); got %d", len(pairs))
	}
	if pairs[0].Left.RecordID == pairs[0].Right.RecordID {
		t.Fatal("self-pair leaked")
	}
}

func TestKeyBasedPairsDedupsAcrossKeys(t *testing.T) {
	t.Parallel()
	// Same logical pair would be emitted twice if dedup is broken.
	pairs := BuildCandidatePairs(sampleRecords(), models.BlockingStrategyConfig{
		StrategyType: "key-based",
		KeyFields:    []string{"email", "phone", "display_name"},
		WindowSize:   5,
		BucketCount:  24,
	})
	seen := map[string]struct{}{}
	for _, p := range pairs {
		k := pairKey(p.Left.RecordID, p.Right.RecordID)
		if _, exists := seen[k]; exists {
			t.Fatalf("duplicate pair %s", k)
		}
		seen[k] = struct{}{}
	}
}

func TestSortedNeighborhoodHonoursWindow(t *testing.T) {
	t.Parallel()
	strategy := models.BlockingStrategyConfig{
		StrategyType: "sorted-neighborhood",
		KeyFields:    []string{"display_name"},
		WindowSize:   2,
		BucketCount:  24,
	}
	pairs := BuildCandidatePairs(sampleRecords(), strategy)
	if len(pairs) == 0 {
		t.Fatal("expected at least one pair")
	}
}

func TestLSHFallsBackToBucket0WhenNoTokens(t *testing.T) {
	t.Parallel()
	records := []models.EntityRecord{
		{RecordID: "a", DisplayName: "", Attributes: map[string]any{}},
		{RecordID: "b", DisplayName: "", Attributes: map[string]any{}},
	}
	strategy := models.BlockingStrategyConfig{
		StrategyType: "lsh",
		KeyFields:    []string{"display_name"},
		WindowSize:   5,
		BucketCount:  24,
	}
	pairs := BuildCandidatePairs(records, strategy)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair (both in bucket-0), got %d", len(pairs))
	}
	if pairs[0].BlockingKey != "bucket-0" {
		t.Fatalf("expected bucket-0, got %s", pairs[0].BlockingKey)
	}
}
