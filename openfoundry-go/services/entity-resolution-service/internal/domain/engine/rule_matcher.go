package engine

import (
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// EvaluateCandidate ports `rule_matcher::evaluate_candidate`.
//
// Returns the MatchEvidence with rule_score / final_score = rule_score
// and ml_score=0; the caller (jobs handler) blends in the ML score.
func EvaluateCandidate(rule models.MatchRule, pair CandidatePair) models.MatchEvidence {
	totalWeight := float32(0)
	for _, c := range rule.Conditions {
		w := c.Weight
		if w < 0 {
			w = 0
		}
		totalWeight += w
	}
	if totalWeight < 1.0 {
		totalWeight = 1.0
	}

	matchedWeight := float32(0)
	requiredMiss := false
	explanations := make([]string, 0, len(rule.Conditions))
	for _, condition := range rule.Conditions {
		score, explanation := scoreCondition(condition, pair)
		if score >= condition.Threshold {
			w := condition.Weight
			if w < 0 {
				w = 0
			}
			matchedWeight += w
		} else if condition.Required {
			requiredMiss = true
		}
		explanations = append(explanations, explanation)
	}

	ruleScore := clamp01(matchedWeight / totalWeight)
	if requiredMiss {
		ruleScore *= 0.45
	}

	return models.MatchEvidence{
		LeftRecordID:   pair.Left.RecordID,
		RightRecordID:  pair.Right.RecordID,
		BlockingKey:    pair.BlockingKey,
		RuleScore:      ruleScore,
		MLScore:        0,
		FinalScore:     ruleScore,
		Comparators:    append([]string{}, explanations...),
		Explanation:    strings.Join(explanations, "; "),
		RequiresReview: false,
	}
}

func scoreCondition(condition models.MatchCondition, pair CandidatePair) (float32, string) {
	left := fieldValue(pair.Left, condition.Field)
	right := fieldValue(pair.Right, condition.Field)
	score := CompareValues(condition.Comparator, left, right)
	explanation := fmt.Sprintf("%s:%s=%.2f", condition.Field, condition.Comparator, score)
	return score, explanation
}

func fieldValue(record models.EntityRecord, field string) string {
	switch field {
	case "display_name", "name":
		return record.DisplayName
	default:
		v, ok := record.Attributes[field]
		if !ok {
			return ""
		}
		s, ok := v.(string)
		if !ok {
			return ""
		}
		return s
	}
}
