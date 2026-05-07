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

// GoldenRecordRepo wraps fusion_golden_records.
type GoldenRecordRepo struct {
	Pool *pgxpool.Pool
}

const goldenRecordColumns = `id, cluster_id, title, canonical_values, provenance,
        completeness_score, confidence_score, status, created_at, updated_at`

func (r *GoldenRecordRepo) List(ctx context.Context) ([]models.GoldenRecord, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+goldenRecordColumns+`
		   FROM fusion_golden_records
		  ORDER BY updated_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.GoldenRecord, 0)
	for rows.Next() {
		gr, err := scanGoldenRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, gr)
	}
	return out, rows.Err()
}

// LatestForCluster returns the most-recently-updated golden record for
// the cluster, or nil if none exists.
func (r *GoldenRecordRepo) LatestForCluster(ctx context.Context, clusterID uuid.UUID) (*models.GoldenRecord, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+goldenRecordColumns+`
		   FROM fusion_golden_records
		  WHERE cluster_id = $1
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		clusterID,
	)
	gr, err := scanGoldenRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &gr, nil
}

func (r *GoldenRecordRepo) Insert(ctx context.Context, gr models.GoldenRecord) error {
	canonicalJSON, err := json.Marshal(gr.CanonicalValues)
	if err != nil {
		return err
	}
	provenanceJSON, err := json.Marshal(gr.Provenance)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx,
		`INSERT INTO fusion_golden_records
		      (id, cluster_id, title, canonical_values, provenance,
		       completeness_score, confidence_score, status,
		       created_at, updated_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		gr.ID, gr.ClusterID, gr.Title, canonicalJSON, provenanceJSON,
		gr.CompletenessScore, gr.ConfidenceScore, gr.Status,
		gr.CreatedAt, gr.UpdatedAt,
	)
	return err
}

// SetStatusByCluster ports the side-effect from submit_review:
// updates every golden record sharing a cluster_id to a new status.
func (r *GoldenRecordRepo) SetStatusByCluster(ctx context.Context, clusterID uuid.UUID, status string) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE fusion_golden_records
		    SET status = $2, updated_at = NOW()
		  WHERE cluster_id = $1`,
		clusterID, status,
	)
	return err
}

func scanGoldenRecord(s rowScanner) (models.GoldenRecord, error) {
	var gr models.GoldenRecord
	var canonicalJSON, provenanceJSON []byte
	if err := s.Scan(
		&gr.ID, &gr.ClusterID, &gr.Title,
		&canonicalJSON, &provenanceJSON,
		&gr.CompletenessScore, &gr.ConfidenceScore, &gr.Status,
		&gr.CreatedAt, &gr.UpdatedAt,
	); err != nil {
		return gr, err
	}
	if err := json.Unmarshal(canonicalJSON, &gr.CanonicalValues); err != nil {
		return gr, err
	}
	if err := json.Unmarshal(provenanceJSON, &gr.Provenance); err != nil {
		return gr, err
	}
	return gr, nil
}
