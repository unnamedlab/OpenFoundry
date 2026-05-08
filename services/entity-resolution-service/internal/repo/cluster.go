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

// ClusterRepo wraps fusion_clusters.
type ClusterRepo struct {
	Pool *pgxpool.Pool
}

const clusterColumns = `id, job_id, cluster_key, status, records, evidence,
        confidence_score, requires_review, suggested_golden_record_id,
        created_at, updated_at`

func (r *ClusterRepo) List(ctx context.Context) ([]models.ResolvedCluster, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+clusterColumns+`
		   FROM fusion_clusters
		  ORDER BY updated_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ResolvedCluster, 0)
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ClusterRepo) Get(ctx context.Context, id uuid.UUID) (*models.ResolvedCluster, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+clusterColumns+` FROM fusion_clusters WHERE id = $1`, id,
	)
	c, err := scanCluster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteByJob removes every cluster row for a given job_id.
func (r *ClusterRepo) DeleteByJob(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM fusion_clusters WHERE job_id = $1`, jobID)
	return err
}

// Insert persists a single cluster row, preserving the in-memory created_at/updated_at.
func (r *ClusterRepo) Insert(ctx context.Context, c models.ResolvedCluster) error {
	recordsJSON, err := json.Marshal(c.Records)
	if err != nil {
		return err
	}
	evidenceJSON, err := json.Marshal(c.Evidence)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx,
		`INSERT INTO fusion_clusters
		      (id, job_id, cluster_key, status, records, evidence,
		       confidence_score, requires_review, suggested_golden_record_id,
		       created_at, updated_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		c.ID, c.JobID, c.ClusterKey, c.Status, recordsJSON, evidenceJSON,
		c.ConfidenceScore, c.RequiresReview, c.SuggestedGoldenRecordID,
		c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// UpdateAfterReview ports the partial UPDATE used by submit_review.
func (r *ClusterRepo) UpdateAfterReview(ctx context.Context, id uuid.UUID, status string, requiresReview bool, suggestedGoldenRecordID *uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE fusion_clusters
		    SET status = $2, requires_review = $3, suggested_golden_record_id = $4,
		        updated_at = NOW()
		  WHERE id = $1`,
		id, status, requiresReview, suggestedGoldenRecordID,
	)
	return err
}

func scanCluster(s rowScanner) (models.ResolvedCluster, error) {
	var c models.ResolvedCluster
	var recordsJSON, evidenceJSON []byte
	var suggested *uuid.UUID
	if err := s.Scan(
		&c.ID, &c.JobID, &c.ClusterKey, &c.Status,
		&recordsJSON, &evidenceJSON,
		&c.ConfidenceScore, &c.RequiresReview, &suggested,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return c, err
	}
	if err := json.Unmarshal(recordsJSON, &c.Records); err != nil {
		return c, err
	}
	if err := json.Unmarshal(evidenceJSON, &c.Evidence); err != nil {
		return c, err
	}
	c.SuggestedGoldenRecordID = suggested
	return c, nil
}
