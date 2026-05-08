package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

func (r *Repository) ListPipelines(ctx context.Context, query models.ListPipelinesQuery) (models.ListPipelinesResponse, error) {
	page := int64(1)
	if query.Page != nil && *query.Page > 0 {
		page = *query.Page
	}
	perPage := int64(50)
	if query.PerPage != nil && *query.PerPage > 0 {
		perPage = *query.PerPage
	}
	if perPage > 200 {
		perPage = 200
	}
	search, status := "", ""
	if query.Search != nil {
		search = strings.TrimSpace(*query.Search)
	}
	if query.Status != nil {
		status = strings.TrimSpace(*query.Status)
	}
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM pipelines WHERE ($1='' OR name ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%') AND ($2='' OR status=$2)`, search, status).Scan(&total); err != nil {
		return models.ListPipelinesResponse{}, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, name, description, owner_id, dag, status, schedule_config, retry_policy, next_run_at, created_at, updated_at
FROM pipelines
WHERE ($1='' OR name ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%') AND ($2='' OR status=$2)
ORDER BY updated_at DESC, created_at DESC
LIMIT $3 OFFSET $4`, search, status, perPage, (page-1)*perPage)
	if err != nil {
		return models.ListPipelinesResponse{}, err
	}
	defer rows.Close()
	items := []models.Pipeline{}
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return models.ListPipelinesResponse{}, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return models.ListPipelinesResponse{}, err
	}
	return models.ListPipelinesResponse{Data: items, Total: total, Page: page, PerPage: perPage}, nil
}

func (r *Repository) CreatePipeline(ctx context.Context, req models.CreatePipelineRequest, ownerID uuid.UUID) (*models.Pipeline, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	status := "draft"
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		status = strings.TrimSpace(*req.Status)
	}
	dag, err := json.Marshal(req.Nodes)
	if err != nil {
		return nil, fmt.Errorf("encode nodes: %w", err)
	}
	scheduleConfig := json.RawMessage(`{}`)
	if req.ScheduleConfig != nil {
		scheduleConfig, err = json.Marshal(req.ScheduleConfig)
		if err != nil {
			return nil, fmt.Errorf("encode schedule_config: %w", err)
		}
	}
	retryPolicy := models.DefaultPipelineRetryPolicy()
	if req.RetryPolicy != nil {
		retryPolicy = *req.RetryPolicy
	}
	retryPolicyRaw, err := json.Marshal(retryPolicy)
	if err != nil {
		return nil, fmt.Errorf("encode retry_policy: %w", err)
	}
	id := uuid.New()
	return r.insertPipeline(ctx, id, req.Name, description, ownerID, dag, status, scheduleConfig, retryPolicyRaw)
}

func (r *Repository) GetPipeline(ctx context.Context, id uuid.UUID) (*models.Pipeline, error) {
	return r.LoadPipeline(ctx, id)
}

func (r *Repository) UpdatePipeline(ctx context.Context, id uuid.UUID, req models.UpdatePipelineRequest) (*models.Pipeline, error) {
	current, err := r.LoadPipeline(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	description := current.Description
	if req.Description != nil {
		description = *req.Description
	}
	status := current.Status
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		status = strings.TrimSpace(*req.Status)
	}
	dag := current.DAG
	if req.Nodes != nil {
		dag, err = json.Marshal(*req.Nodes)
		if err != nil {
			return nil, fmt.Errorf("encode nodes: %w", err)
		}
	}
	scheduleConfig := current.ScheduleConfig
	if req.ScheduleConfig != nil {
		scheduleConfig, err = json.Marshal(req.ScheduleConfig)
		if err != nil {
			return nil, fmt.Errorf("encode schedule_config: %w", err)
		}
	}
	retryPolicy := current.RetryPolicy
	if req.RetryPolicy != nil {
		retryPolicy, err = json.Marshal(req.RetryPolicy)
		if err != nil {
			return nil, fmt.Errorf("encode retry_policy: %w", err)
		}
	}
	var p models.Pipeline
	err = r.db.QueryRow(ctx, `UPDATE pipelines
SET name=$2, description=$3, dag=$4, status=$5, schedule_config=$6, retry_policy=$7, updated_at=NOW()
WHERE id=$1
RETURNING id, name, description, owner_id, dag, status, schedule_config, retry_policy, next_run_at, created_at, updated_at`, id, name, description, dag, status, scheduleConfig, retryPolicy).Scan(&p.ID, &p.Name, &p.Description, &p.OwnerID, &p.DAG, &p.Status, &p.ScheduleConfig, &p.RetryPolicy, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

func (r *Repository) DeletePipeline(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM pipelines WHERE id=$1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) insertPipeline(ctx context.Context, id uuid.UUID, name, description string, ownerID uuid.UUID, dag json.RawMessage, status string, scheduleConfig json.RawMessage, retryPolicy json.RawMessage) (*models.Pipeline, error) {
	var p models.Pipeline
	err := r.db.QueryRow(ctx, `INSERT INTO pipelines (id, name, description, owner_id, dag, status, schedule_config, retry_policy)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING id, name, description, owner_id, dag, status, schedule_config, retry_policy, next_run_at, created_at, updated_at`, id, name, description, ownerID, dag, status, scheduleConfig, retryPolicy).Scan(&p.ID, &p.Name, &p.Description, &p.OwnerID, &p.DAG, &p.Status, &p.ScheduleConfig, &p.RetryPolicy, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt)
	return &p, err
}

type pipelineScanner interface {
	Scan(dest ...any) error
}

func scanPipeline(row pipelineScanner) (models.Pipeline, error) {
	var p models.Pipeline
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.OwnerID, &p.DAG, &p.Status, &p.ScheduleConfig, &p.RetryPolicy, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
