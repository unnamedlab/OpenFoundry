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

// MatchRuleRepo is a thin pgx wrapper for fusion_match_rules.
type MatchRuleRepo struct {
	Pool *pgxpool.Pool
}

func (r *MatchRuleRepo) List(ctx context.Context) ([]models.MatchRule, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, name, description, status, entity_type,
                blocking_strategy, conditions, review_threshold,
                auto_merge_threshold, created_at, updated_at
           FROM fusion_match_rules
          ORDER BY updated_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MatchRule, 0)
	for rows.Next() {
		mr, err := scanMatchRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mr)
	}
	return out, rows.Err()
}

func (r *MatchRuleRepo) Get(ctx context.Context, id uuid.UUID) (*models.MatchRule, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, name, description, status, entity_type,
                blocking_strategy, conditions, review_threshold,
                auto_merge_threshold, created_at, updated_at
           FROM fusion_match_rules WHERE id = $1`,
		id,
	)
	mr, err := scanMatchRule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mr, nil
}

func (r *MatchRuleRepo) Create(ctx context.Context, body models.CreateMatchRuleRequest) (models.MatchRule, error) {
	desc := derefStr(body.Description, "")
	status := derefStr(body.Status, "active")
	entityType := derefStr(body.EntityType, "person")
	blocking := models.DefaultBlockingStrategy()
	if body.BlockingStrategy != nil {
		blocking = *body.BlockingStrategy
	}
	reviewThreshold := derefF32(body.ReviewThreshold, 0.76)
	autoMergeThreshold := derefF32(body.AutoMergeThreshold, 0.9)

	blockingJSON, err := json.Marshal(blocking)
	if err != nil {
		return models.MatchRule{}, err
	}
	conditionsJSON, err := json.Marshal(body.Conditions)
	if err != nil {
		return models.MatchRule{}, err
	}

	row := r.Pool.QueryRow(ctx,
		`INSERT INTO fusion_match_rules
              (id, name, description, status, entity_type,
               blocking_strategy, conditions, review_threshold, auto_merge_threshold)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
            RETURNING id, name, description, status, entity_type,
                      blocking_strategy, conditions, review_threshold,
                      auto_merge_threshold, created_at, updated_at`,
		engine.MustNewUUIDv7(), trimStr(body.Name), desc, status, entityType,
		blockingJSON, conditionsJSON, reviewThreshold, autoMergeThreshold,
	)
	return scanMatchRule(row)
}

func (r *MatchRuleRepo) Update(ctx context.Context, id uuid.UUID, body models.UpdateMatchRuleRequest, current models.MatchRule) (models.MatchRule, error) {
	name := derefStrPtr(body.Name, current.Name)
	desc := derefStrPtr(body.Description, current.Description)
	status := derefStrPtr(body.Status, current.Status)
	entityType := derefStrPtr(body.EntityType, current.EntityType)
	blocking := current.BlockingStrategy
	if body.BlockingStrategy != nil {
		blocking = *body.BlockingStrategy
	}
	conditions := current.Conditions
	if body.Conditions != nil {
		conditions = *body.Conditions
	}
	reviewThreshold := derefF32Ptr(body.ReviewThreshold, current.ReviewThreshold)
	autoMergeThreshold := derefF32Ptr(body.AutoMergeThreshold, current.AutoMergeThreshold)

	blockingJSON, err := json.Marshal(blocking)
	if err != nil {
		return models.MatchRule{}, err
	}
	conditionsJSON, err := json.Marshal(conditions)
	if err != nil {
		return models.MatchRule{}, err
	}

	row := r.Pool.QueryRow(ctx,
		`UPDATE fusion_match_rules
            SET name = $2, description = $3, status = $4, entity_type = $5,
                blocking_strategy = $6, conditions = $7,
                review_threshold = $8, auto_merge_threshold = $9,
                updated_at = NOW()
          WHERE id = $1
          RETURNING id, name, description, status, entity_type,
                    blocking_strategy, conditions, review_threshold,
                    auto_merge_threshold, created_at, updated_at`,
		id, name, desc, status, entityType,
		blockingJSON, conditionsJSON, reviewThreshold, autoMergeThreshold,
	)
	return scanMatchRule(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMatchRule(s rowScanner) (models.MatchRule, error) {
	var mr models.MatchRule
	var blockingJSON, conditionsJSON []byte
	if err := s.Scan(
		&mr.ID, &mr.Name, &mr.Description, &mr.Status, &mr.EntityType,
		&blockingJSON, &conditionsJSON,
		&mr.ReviewThreshold, &mr.AutoMergeThreshold,
		&mr.CreatedAt, &mr.UpdatedAt,
	); err != nil {
		return mr, err
	}
	if err := json.Unmarshal(blockingJSON, &mr.BlockingStrategy); err != nil {
		return mr, err
	}
	if err := json.Unmarshal(conditionsJSON, &mr.Conditions); err != nil {
		return mr, err
	}
	return mr, nil
}

// helpers
func deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}
func derefStr(p *string, fallback string) string  { return deref(p, fallback) }
func derefStrPtr(p *string, fallback string) string { return deref(p, fallback) }
func derefF32(p *float32, fallback float32) float32 { return deref(p, fallback) }
func derefF32Ptr(p *float32, fallback float32) float32 { return deref(p, fallback) }

func trimStr(s string) string {
	out := []rune(s)
	start, end := 0, len(out)
	for start < end && isSpace(out[start]) {
		start++
	}
	for end > start && isSpace(out[end-1]) {
		end--
	}
	return string(out[start:end])
}
func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }
