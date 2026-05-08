package engine

import "github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"

// ScoreCandidate ports `ml_matcher::score_candidate`.
func ScoreCandidate(left, right models.EntityRecord, evidence models.MatchEvidence) float32 {
	nameScore := CompareValues("fuzzy", left.DisplayName, right.DisplayName)
	emailScore := CompareValues("email_exact", attribute(left, "email"), attribute(right, "email"))
	phoneScore := float32(0)
	if NormalizePhone(attribute(left, "phone")) == NormalizePhone(attribute(right, "phone")) {
		phoneScore = 1.0
	}
	companyScore := CompareValues("fuzzy", attribute(left, "company"), attribute(right, "company"))
	cityScore := CompareValues("exact", attribute(left, "city"), attribute(right, "city"))

	return clamp01(0.45*evidence.RuleScore +
		0.20*nameScore +
		0.15*emailScore +
		0.10*phoneScore +
		0.05*companyScore +
		0.05*cityScore)
}

// BlendScores ports `ml_matcher::blend_scores`.
func BlendScores(ruleScore, mlScore float32) float32 {
	return clamp01(0.65*ruleScore + 0.35*mlScore)
}

func attribute(record models.EntityRecord, field string) string {
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
