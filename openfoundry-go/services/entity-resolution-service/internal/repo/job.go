package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/domain/engine"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// FusionJobRepo wraps fusion_jobs.
type FusionJobRepo struct {
	Pool *pgxpool.Pool
}

const fusionJobColumns = `id, name, description, status, entity_type,
        match_rule_id, merge_strategy_id, config, metrics,
        last_run_summary, last_run_at, created_at, updated_at`

func (r *FusionJobRepo) List(ctx context.Context) ([]models.FusionJob, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+fusionJobColumns+`
		   FROM fusion_jobs
		  ORDER BY updated_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.FusionJob, 0)
	for rows.Next() {
		j, err := scanFusionJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r *FusionJobRepo) Get(ctx context.Context, id uuid.UUID) (*models.FusionJob, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+fusionJobColumns+` FROM fusion_jobs WHERE id = $1`, id,
	)
	j, err := scanFusionJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *FusionJobRepo) Create(ctx context.Context, body models.CreateFusionJobRequest) (models.FusionJob, error) {
	desc := derefStr(body.Description, "")
	status := derefStr(body.Status, "draft")
	entityType := derefStr(body.EntityType, "person")
	cfg := models.DefaultResolutionJobConfig()
	if body.Config != nil {
		cfg = *body.Config
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return models.FusionJob{}, err
	}
	metricsJSON, err := json.Marshal(models.FusionJobMetrics{})
	if err != nil {
		return models.FusionJob{}, err
	}

	row := r.Pool.QueryRow(ctx,
		`INSERT INTO fusion_jobs
		      (id, name, description, status, entity_type,
		       match_rule_id, merge_strategy_id, config, metrics,
		       last_run_summary, last_run_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'Not run yet', NULL)
		    RETURNING `+fusionJobColumns,
		engine.MustNewUUIDv7(), trimStr(body.Name), desc, status, entityType,
		body.MatchRuleID, body.MergeStrategyID, configJSON, metricsJSON,
	)
	return scanFusionJob(row)
}

// UpdateAfterRun applies metrics + summary + status + last_run_at.
func (r *FusionJobRepo) UpdateAfterRun(ctx context.Context, jobID uuid.UUID, status, summary string, metrics models.FusionJobMetrics) (models.FusionJob, error) {
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return models.FusionJob{}, err
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE fusion_jobs
		    SET status = $2, metrics = $3, last_run_summary = $4,
		        last_run_at = NOW(), updated_at = NOW()
		  WHERE id = $1
		  RETURNING `+fusionJobColumns,
		jobID, status, metricsJSON, summary,
	)
	return scanFusionJob(row)
}

func scanFusionJob(s rowScanner) (models.FusionJob, error) {
	var j models.FusionJob
	var configJSON, metricsJSON []byte
	var lastRunAt *time.Time
	if err := s.Scan(
		&j.ID, &j.Name, &j.Description, &j.Status, &j.EntityType,
		&j.MatchRuleID, &j.MergeStrategyID,
		&configJSON, &metricsJSON,
		&j.LastRunSummary, &lastRunAt,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return j, err
	}
	if err := json.Unmarshal(configJSON, &j.Config); err != nil {
		return j, err
	}
	if err := json.Unmarshal(metricsJSON, &j.Metrics); err != nil {
		return j, err
	}
	j.LastRunAt = lastRunAt
	return j, nil
}
