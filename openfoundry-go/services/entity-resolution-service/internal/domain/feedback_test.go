package domain

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

func ptrStr(s string) *string { return &s }

func TestApplyReviewBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		decision    string
		wantCluster string
		wantReview  string
	}{
		{"confirm_match", "resolved", "resolved"},
		{"split_cluster", "split_requested", "split_requested"},
		{"reject_match", "rejected", "rejected"},
		{"anything_else", "manually_resolved", "resolved"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.decision, func(t *testing.T) {
			t.Parallel()
			cluster := models.ResolvedCluster{Status: "pending_review", RequiresReview: true}
			review := &models.ReviewQueueItem{Status: "pending"}
			updatedCluster, updatedReview := ApplyReview(cluster, review, models.SubmitReviewRequest{
				Decision:   tc.decision,
				Notes:      ptrStr("noted"),
				ReviewedBy: ptrStr("op"),
			})
			if updatedCluster.Status != tc.wantCluster {
				t.Fatalf("cluster status got %s want %s", updatedCluster.Status, tc.wantCluster)
			}
			if updatedCluster.RequiresReview {
				t.Fatal("requires_review must be cleared")
			}
			if updatedReview == nil {
				t.Fatal("review should be returned")
			}
			if updatedReview.Status != tc.wantReview {
				t.Fatalf("review status got %s want %s", updatedReview.Status, tc.wantReview)
			}
			if updatedReview.Notes != "noted" {
				t.Fatalf("notes got %q", updatedReview.Notes)
			}
			if updatedReview.ReviewedBy == nil || *updatedReview.ReviewedBy != "op" {
				t.Fatalf("reviewed_by got %v", updatedReview.ReviewedBy)
			}
		})
	}
}

func TestApplyReviewWithoutExistingReview(t *testing.T) {
	t.Parallel()
	cluster := models.ResolvedCluster{Status: "pending_review"}
	updatedCluster, updatedReview := ApplyReview(cluster, nil, models.SubmitReviewRequest{Decision: "confirm_match"})
	if updatedCluster.Status != "resolved" {
		t.Fatalf("got %s", updatedCluster.Status)
	}
	if updatedReview != nil {
		t.Fatal("no review should be returned")
	}
}

func TestApplyReviewNotesEmptyWhenAbsent(t *testing.T) {
	t.Parallel()
	cluster := models.ResolvedCluster{}
	review := &models.ReviewQueueItem{Notes: "old"}
	_, updated := ApplyReview(cluster, review, models.SubmitReviewRequest{Decision: "confirm_match"})
	if updated.Notes != "" {
		t.Fatalf("expected empty notes, got %q", updated.Notes)
	}
}
