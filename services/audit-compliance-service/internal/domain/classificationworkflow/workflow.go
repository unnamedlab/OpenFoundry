package classificationworkflow

import (
	"strings"
	"time"
)

type SensitivityCategory string

const (
	CategoryPII            SensitivityCategory = "pii"
	CategoryPHI            SensitivityCategory = "phi"
	CategoryCUI            SensitivityCategory = "cui"
	CategoryClassifiedLike SensitivityCategory = "classified_like"
)

type ClassificationRequest struct {
	DatasetRID            string
	DetectedCategories    []SensitivityCategory
	ExistingMarkings      []string
	DataOwner             string
	SensitivityRationale  string
	TrainingPrerequisites []string
	AccessPrerequisites   []string
	ReviewCadenceDays     int
	Now                   time.Time
}

type WorkflowStep struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Required bool     `json:"required"`
	Details  []string `json:"details"`
}

type ClassificationPlan struct {
	DatasetRID            string                `json:"dataset_rid"`
	DataOwner             string                `json:"data_owner"`
	SensitivityRationale  string                `json:"sensitivity_rationale"`
	Categories            []SensitivityCategory `json:"categories"`
	RecommendedMarkings   []string              `json:"recommended_markings"`
	ProjectTemplateKeys   []string              `json:"project_template_keys"`
	TrainingPrerequisites []string              `json:"training_prerequisites"`
	AccessPrerequisites   []string              `json:"access_prerequisites"`
	ReviewCadenceDays     int                   `json:"review_cadence_days"`
	NextReviewAt          time.Time             `json:"next_review_at"`
	Steps                 []WorkflowStep        `json:"steps"`
	BlockingReasons       []string              `json:"blocking_reasons"`
}

func BuildClassificationPlan(req ClassificationRequest) ClassificationPlan {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cadence := req.ReviewCadenceDays
	if cadence <= 0 {
		cadence = 90
	}
	plan := ClassificationPlan{
		DatasetRID:            strings.TrimSpace(req.DatasetRID),
		DataOwner:             strings.TrimSpace(req.DataOwner),
		SensitivityRationale:  strings.TrimSpace(req.SensitivityRationale),
		Categories:            normalizeCategories(req.DetectedCategories),
		RecommendedMarkings:   normalizeStrings(req.ExistingMarkings),
		TrainingPrerequisites: normalizeStrings(req.TrainingPrerequisites),
		AccessPrerequisites:   normalizeStrings(req.AccessPrerequisites),
		ReviewCadenceDays:     cadence,
		NextReviewAt:          now.Add(time.Duration(cadence) * 24 * time.Hour),
	}
	for _, category := range plan.Categories {
		plan.RecommendedMarkings = append(plan.RecommendedMarkings, defaultMarking(category))
		plan.ProjectTemplateKeys = append(plan.ProjectTemplateKeys, "secure-"+string(category))
	}
	plan.RecommendedMarkings = normalizeStrings(plan.RecommendedMarkings)
	plan.ProjectTemplateKeys = normalizeStrings(plan.ProjectTemplateKeys)
	plan.Steps = []WorkflowStep{
		{ID: "identify-sensitive-dataset", Title: "Identify sensitive dataset", Required: true, Details: []string{plan.DatasetRID}},
		{ID: "assign-markings", Title: "Assign mandatory markings", Required: true, Details: plan.RecommendedMarkings},
		{ID: "create-restricted-views", Title: "Create restricted views for least-privilege access", Required: true, Details: []string{"mask direct dataset access where possible"}},
		{ID: "set-retention", Title: "Set retention policy and review cadence", Required: true, Details: []string{plan.NextReviewAt.Format(time.RFC3339)}},
		{ID: "configure-export-egress", Title: "Configure export and egress limitations", Required: true, Details: []string{"require justifications", "block unapproved destinations"}},
	}
	if plan.DatasetRID == "" {
		plan.BlockingReasons = append(plan.BlockingReasons, "dataset_rid is required")
	}
	if plan.DataOwner == "" {
		plan.BlockingReasons = append(plan.BlockingReasons, "data owner is required")
	}
	if plan.SensitivityRationale == "" {
		plan.BlockingReasons = append(plan.BlockingReasons, "sensitivity rationale is required")
	}
	if len(plan.Categories) == 0 {
		plan.BlockingReasons = append(plan.BlockingReasons, "at least one sensitivity category is required")
	}
	return plan
}

func defaultMarking(category SensitivityCategory) string {
	switch category {
	case CategoryPII:
		return "marking:pii"
	case CategoryPHI:
		return "marking:phi"
	case CategoryCUI:
		return "marking:cui"
	case CategoryClassifiedLike:
		return "marking:classified-like"
	default:
		return "marking:" + strings.TrimSpace(string(category))
	}
}

func normalizeCategories(values []SensitivityCategory) []SensitivityCategory {
	out := []SensitivityCategory{}
	seen := map[string]struct{}{}
	for _, value := range values {
		v := SensitivityCategory(strings.ToLower(strings.TrimSpace(string(value))))
		if v == "" {
			continue
		}
		if _, ok := seen[string(v)]; ok {
			continue
		}
		seen[string(v)] = struct{}{}
		out = append(out, v)
	}
	return out
}

func normalizeStrings(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
