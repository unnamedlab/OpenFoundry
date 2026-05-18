package classificationworkflow

import (
	"testing"
	"time"
)

func TestBuildClassificationPlanCreatesGovernanceWorkflow(t *testing.T) {
	now := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	plan := BuildClassificationPlan(ClassificationRequest{
		DatasetRID:            "dataset-sensitive",
		DetectedCategories:    []SensitivityCategory{CategoryPII, CategoryPHI, CategoryPII},
		ExistingMarkings:      []string{"marking:internal"},
		DataOwner:             "owner@example.com",
		SensitivityRationale:  "contains patient contact data",
		TrainingPrerequisites: []string{"privacy-101"},
		AccessPrerequisites:   []string{"manager approval"},
		ReviewCadenceDays:     30,
		Now:                   now,
	})
	if len(plan.BlockingReasons) != 0 {
		t.Fatalf("unexpected blockers: %#v", plan.BlockingReasons)
	}
	for _, want := range []string{"marking:internal", "marking:pii", "marking:phi"} {
		if !contains(plan.RecommendedMarkings, want) {
			t.Fatalf("markings %#v missing %q", plan.RecommendedMarkings, want)
		}
	}
	for _, want := range []string{"secure-pii", "secure-phi"} {
		if !contains(plan.ProjectTemplateKeys, want) {
			t.Fatalf("templates %#v missing %q", plan.ProjectTemplateKeys, want)
		}
	}
	if !plan.NextReviewAt.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("next review=%s", plan.NextReviewAt)
	}
	if len(plan.Steps) != 5 {
		t.Fatalf("steps=%#v", plan.Steps)
	}
}

func TestBuildClassificationPlanRequiresOwnerRationaleAndCategory(t *testing.T) {
	plan := BuildClassificationPlan(ClassificationRequest{DatasetRID: "dataset"})
	for _, want := range []string{"data owner is required", "sensitivity rationale is required", "at least one sensitivity category is required"} {
		if !contains(plan.BlockingReasons, want) {
			t.Fatalf("blockers %#v missing %q", plan.BlockingReasons, want)
		}
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
