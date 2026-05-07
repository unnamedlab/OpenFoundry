package repo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/domain/engine"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

type MergeStrategyRepo struct {
	Pool *pgxpool.Pool
}

func (r *MergeStrategyRepo) List(ctx context.Context) ([]models.MergeStrategy, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, name, description, status, entity_type,
                default_strategy, rules, created_at, updated_at
           FROM fusion_merge_strategies
          ORDER BY updated_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MergeStrategy, 0)
	for rows.Next() {
		ms, err := scanMergeStrategy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ms)
	}
	return out, rows.Err()
}

func (r *MergeStrategyRepo) Get(ctx context.Context, id uuid.UUID) (*models.MergeStrategy, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, name, description, status, entity_type,
                default_strategy, rules, created_at, updated_at
           FROM fusion_merge_strategies WHERE id = $1`,
		id,
	)
	ms, err := scanMergeStrategy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ms, nil
}

func (r *MergeStrategyRepo) Create(ctx context.Context, body models.CreateMergeStrategyRequest) (models.MergeStrategy, error) {
	desc := derefStr(body.Description, "")
	status := derefStr(body.Status, "active")
	entityType := derefStr(body.EntityType, "person")
	defaultStrategy := derefStr(body.DefaultStrategy, "longest_non_empty")

	rulesJSON, err := json.Marshal(body.Rules)
	if err != nil {
		return models.MergeStrategy{}, err
	}

	row := r.Pool.QueryRow(ctx,
		`INSERT INTO fusion_merge_strategies
              (id, name, description, status, entity_type, default_strategy, rules)
            VALUES ($1, $2, $3, $4, $5, $6, $7)
            RETURNING id, name, description, status, entity_type,
                      default_strategy, rules, created_at, updated_at`,
		engine.MustNewUUIDv7(), trimStr(body.Name), desc, status, entityType, defaultStrategy, rulesJSON,
	)
	return scanMergeStrategy(row)
}

func (r *MergeStrategyRepo) Update(ctx context.Context, id uuid.UUID, body models.UpdateMergeStrategyRequest, current models.MergeStrategy) (models.MergeStrategy, error) {
	name := derefStrPtr(body.Name, current.Name)
	desc := derefStrPtr(body.Description, current.Description)
	status := derefStrPtr(body.Status, current.Status)
	entityType := derefStrPtr(body.EntityType, current.EntityType)
	defaultStrategy := derefStrPtr(body.DefaultStrategy, current.DefaultStrategy)
	rules := current.Rules
	if body.Rules != nil {
		rules = *body.Rules
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return models.MergeStrategy{}, err
	}

	row := r.Pool.QueryRow(ctx,
		`UPDATE fusion_merge_strategies
            SET name = $2, description = $3, status = $4, entity_type = $5,
                default_strategy = $6, rules = $7, updated_at = NOW()
          WHERE id = $1
          RETURNING id, name, description, status, entity_type,
                    default_strategy, rules, created_at, updated_at`,
		id, name, desc, status, entityType, defaultStrategy, rulesJSON,
	)
	return scanMergeStrategy(row)
}

func scanMergeStrategy(s rowScanner) (models.MergeStrategy, error) {
	var ms models.MergeStrategy
	var rulesJSON []byte
	if err := s.Scan(
		&ms.ID, &ms.Name, &ms.Description, &ms.Status, &ms.EntityType,
		&ms.DefaultStrategy, &rulesJSON, &ms.CreatedAt, &ms.UpdatedAt,
	); err != nil {
		return ms, err
	}
	if err := json.Unmarshal(rulesJSON, &ms.Rules); err != nil {
		return ms, err
	}
	return ms, nil
}
