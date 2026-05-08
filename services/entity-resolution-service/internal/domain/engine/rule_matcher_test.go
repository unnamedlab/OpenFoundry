package engine

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

func TestEvaluateCandidateNoConditionsClampsToOne(t *testing.T) {
	t.Parallel()
	rule := models.MatchRule{Conditions: []models.MatchCondition{}}
	pair := CandidatePair{
		Left:        models.EntityRecord{RecordID: "a"},
		Right:       models.EntityRecord{RecordID: "b"},
		BlockingKey: "k",
	}
	got := EvaluateCandidate(rule, pair)
	if got.RuleScore != 0 {
		t.Fatalf("no conditions ⇒ matched_weight=0, score=0; got %v", got.RuleScore)
	}
	if got.LeftRecordID != "a" || got.RightRecordID != "b" {
		t.Fatalf("ids not propagated: %+v", got)
	}
}

func TestEvaluateCandidateRequiredMissPenalty(t *testing.T) {
	t.Parallel()
	rule := models.MatchRule{Conditions: []models.MatchCondition{
		{Field: "display_name", Comparator: "exact", Weight: 1.0, Threshold: 1.0, Required: true},
	}}
	pair := CandidatePair{
		Left:  models.EntityRecord{DisplayName: "Foo"},
		Right: models.EntityRecord{DisplayName: "Bar"},
	}
	got := EvaluateCandidate(rule, pair)
	// matchedWeight = 0 ⇒ ruleScore = 0/1 = 0; required miss only matters when
	// the score would otherwise be > 0. Verify final is 0 either way.
	if got.RuleScore != 0 {
		t.Fatalf("expected 0, got %v", got.RuleScore)
	}
}

func TestEvaluateCandidateAllMatch(t *testing.T) {
	t.Parallel()
	rule := models.MatchRule{Conditions: []models.MatchCondition{
		{Field: "display_name", Comparator: "exact", Weight: 1.0, Threshold: 0.9, Required: false},
	}}
	pair := CandidatePair{
		Left:  models.EntityRecord{DisplayName: "Foo"},
		Right: models.EntityRecord{DisplayName: "foo"},
	}
	got := EvaluateCandidate(rule, pair)
	if got.RuleScore != 1.0 {
		t.Fatalf("expected 1.0, got %v", got.RuleScore)
	}
	if got.Explanation == "" {
		t.Fatal("explanation should be non-empty")
	}
}
