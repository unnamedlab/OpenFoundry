// Package domain ports fusion_base/domain — feedback (apply_review),
// deduplication (synthesize_entity_records), merge (synthesize_golden_record).
package domain

import (
	"time"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// ApplyReview ports `feedback::apply_review`.
//
// Returns updated cluster + optional updated review item. The original
// inputs are not mutated.
func ApplyReview(cluster models.ResolvedCluster, reviewItem *models.ReviewQueueItem, request models.SubmitReviewRequest) (models.ResolvedCluster, *models.ReviewQueueItem) {
	now := time.Now().UTC()

	updated := cluster
	switch request.Decision {
	case "confirm_match":
		updated.Status = "resolved"
		updated.RequiresReview = false
	case "split_cluster":
		updated.Status = "split_requested"
		updated.RequiresReview = false
	case "reject_match":
		updated.Status = "rejected"
		updated.RequiresReview = false
	default:
		updated.Status = "manually_resolved"
		updated.RequiresReview = false
	}
	updated.UpdatedAt = now

	if reviewItem == nil {
		return updated, nil
	}
	updatedReview := *reviewItem
	switch request.Decision {
	case "split_cluster":
		updatedReview.Status = "split_requested"
	case "reject_match":
		updatedReview.Status = "rejected"
	default:
		updatedReview.Status = "resolved"
	}
	updatedReview.ReviewedBy = request.ReviewedBy
	notes := ""
	if request.Notes != nil {
		notes = *request.Notes
	}
	updatedReview.Notes = notes
	updatedReview.UpdatedAt = now

	return updated, &updatedReview
}
