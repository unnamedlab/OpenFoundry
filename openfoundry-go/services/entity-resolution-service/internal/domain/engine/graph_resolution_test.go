package engine

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

func TestResolveClustersSingleton(t *testing.T) {
	t.Parallel()
	jobID := uuid.New()
	records := []models.EntityRecord{
		{RecordID: "a", Confidence: 0.5},
		{RecordID: "b", Confidence: 0.5},
	}
	res := ResolveClusters(jobID, records, nil, 0.7, 0.9)
	if len(res.Clusters) != 2 {
		t.Fatalf("no evidences ⇒ singletons; got %d", len(res.Clusters))
	}
	for _, c := range res.Clusters {
		if c.Status != "singleton" {
			t.Fatalf("expected singleton, got %s", c.Status)
		}
	}
}

func TestResolveClustersMergesAboveThreshold(t *testing.T) {
	t.Parallel()
	jobID := uuid.New()
	records := []models.EntityRecord{
		{RecordID: "a", Confidence: 0.95},
		{RecordID: "b", Confidence: 0.95},
	}
	evidences := []models.MatchEvidence{
		{LeftRecordID: "a", RightRecordID: "b", FinalScore: 0.95},
	}
	res := ResolveClusters(jobID, records, evidences, 0.7, 0.9)
	if len(res.Clusters) != 1 {
		t.Fatalf("expected 1 merged cluster, got %d", len(res.Clusters))
	}
	c := res.Clusters[0]
	if c.Status != "resolved" {
		t.Fatalf("expected resolved, got %s", c.Status)
	}
	if c.RequiresReview {
		t.Fatal("auto-merge above threshold should not require review")
	}
	if len(res.ReviewItems) != 0 {
		t.Fatalf("no review items expected, got %d", len(res.ReviewItems))
	}
}

func TestResolveClustersFlagsReviewWhenInGap(t *testing.T) {
	t.Parallel()
	jobID := uuid.New()
	records := []models.EntityRecord{
		{RecordID: "a", Confidence: 0.5},
		{RecordID: "b", Confidence: 0.5},
	}
	evidences := []models.MatchEvidence{
		{LeftRecordID: "a", RightRecordID: "b", FinalScore: 0.8, Explanation: "name:fuzzy=0.80"},
	}
	res := ResolveClusters(jobID, records, evidences, 0.7, 0.9)
	if len(res.Clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(res.Clusters))
	}
	if !res.Clusters[0].RequiresReview {
		t.Fatal("expected requires_review")
	}
	if len(res.ReviewItems) != 1 {
		t.Fatalf("expected 1 review item, got %d", len(res.ReviewItems))
	}
	if res.ReviewItems[0].Severity != "high" {
		// confidence=0.8 < auto_merge=0.9 ⇒ high severity per Rust source.
		t.Fatalf("expected high severity, got %s", res.ReviewItems[0].Severity)
	}
}
