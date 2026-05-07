package repo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// ReviewQueueRepo wraps fusion_review_queue.
type ReviewQueueRepo struct {
	Pool *pgxpool.Pool
}

const reviewQueueColumns = `id, cluster_id, status, severity, recommended_action,
        rationale, assigned_to, reviewed_by, notes, created_at, updated_at`

func (r *ReviewQueueRepo) List(ctx context.Context) ([]models.ReviewQueueItem, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+reviewQueueColumns+`
		   FROM fusion_review_queue
		  ORDER BY updated_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ReviewQueueItem, 0)
	for rows.Next() {
		it, err := scanReviewQueue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// LatestForCluster returns the most-recently-updated review queue item
// for the given cluster, or nil if none exists.
func (r *ReviewQueueRepo) LatestForCluster(ctx context.Context, clusterID uuid.UUID) (*models.ReviewQueueItem, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+reviewQueueColumns+`
		   FROM fusion_review_queue
		  WHERE cluster_id = $1
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		clusterID,
	)
	it, err := scanReviewQueue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (r *ReviewQueueRepo) Insert(ctx context.Context, item models.ReviewQueueItem) error {
	rationaleJSON, err := json.Marshal(item.Rationale)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx,
		`INSERT INTO fusion_review_queue
		      (id, cluster_id, status, severity, recommended_action,
		       rationale, assigned_to, reviewed_by, notes,
		       created_at, updated_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		item.ID, item.ClusterID, item.Status, item.Severity, item.RecommendedAction,
		rationaleJSON, item.AssignedTo, item.ReviewedBy, item.Notes,
		item.CreatedAt, item.UpdatedAt,
	)
	return err
}

// UpdateAfterReview applies the partial UPDATE used by submit_review.
func (r *ReviewQueueRepo) UpdateAfterReview(ctx context.Context, item models.ReviewQueueItem) error {
	rationaleJSON, err := json.Marshal(item.Rationale)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx,
		`UPDATE fusion_review_queue
		    SET status = $2, reviewed_by = $3, notes = $4, rationale = $5,
		        updated_at = NOW()
		  WHERE id = $1`,
		item.ID, item.Status, item.ReviewedBy, item.Notes, rationaleJSON,
	)
	return err
}

func scanReviewQueue(s rowScanner) (models.ReviewQueueItem, error) {
	var it models.ReviewQueueItem
	var rationaleJSON []byte
	if err := s.Scan(
		&it.ID, &it.ClusterID, &it.Status, &it.Severity, &it.RecommendedAction,
		&rationaleJSON, &it.AssignedTo, &it.ReviewedBy, &it.Notes,
		&it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return it, err
	}
	if err := json.Unmarshal(rationaleJSON, &it.Rationale); err != nil {
		return it, err
	}
	return it, nil
}
