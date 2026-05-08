package repo

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ListCheckpointPolicies: read-only catalog (managed via migrations).
func (r *Repo) ListCheckpointPolicies(ctx context.Context) ([]models.CheckpointPolicy, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT slug, name, interaction_type, sensitivity, enforcement_mode,
		        prompts, rules, created_at, updated_at
		   FROM checkpoint_policies ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CheckpointPolicy, 0)
	for rows.Next() {
		var v models.CheckpointPolicy
		if err := rows.Scan(&v.Slug, &v.Name, &v.InteractionType,
			&v.Sensitivity, &v.EnforcementMode, &v.Prompts, &v.Rules,
			&v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListSensitiveInteractionConfigs: read-only catalog.
func (r *Repo) ListSensitiveInteractionConfigs(ctx context.Context) ([]models.SensitiveInteractionConfig, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT interaction_type, sensitivity, require_purpose_justification,
		        require_auditable_record, linked_policy_slug
		   FROM sensitive_interaction_configs ORDER BY interaction_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SensitiveInteractionConfig, 0)
	for rows.Next() {
		var v models.SensitiveInteractionConfig
		if err := rows.Scan(&v.InteractionType, &v.Sensitivity,
			&v.RequirePurposeJustification, &v.RequireAuditableRecord,
			&v.LinkedPolicySlug); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListPurposeTemplates: read-only catalog.
func (r *Repo) ListPurposeTemplates(ctx context.Context) ([]models.PurposeTemplate, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT slug, name, summary, prompts, required_tags
		   FROM purpose_templates ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.PurposeTemplate, 0)
	for rows.Next() {
		var v models.PurposeTemplate
		if err := rows.Scan(&v.Slug, &v.Name, &v.Summary,
			&v.Prompts, &v.RequiredTags); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreatePurposeRecord writes a ledger row.
func (r *Repo) CreatePurposeRecord(ctx context.Context, body *models.CreatePurposeRecordRequest) (*models.PurposeRecord, error) {
	id := uuid.New()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO purpose_records (id, interaction_type, actor_id,
		     purpose_justification, status, policy_slug, tags, evidence)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, interaction_type, actor_id, purpose_justification,
		           status, policy_slug, tags, evidence, created_at`,
		id, body.InteractionType, body.ActorID,
		body.PurposeJustification, body.Status, body.PolicySlug,
		defaultJSON(body.Tags, "[]"), defaultJSON(body.Evidence, "{}"),
	)
	var v models.PurposeRecord
	if err := row.Scan(&v.ID, &v.InteractionType, &v.ActorID,
		&v.PurposeJustification, &v.Status, &v.PolicySlug,
		&v.Tags, &v.Evidence, &v.CreatedAt); err != nil {
		return nil, err
	}
	return &v, nil
}

// ListPurposeRecordsByInteraction returns the most-recent N records
// for a given interaction_type (default 50, max 500).
func (r *Repo) ListPurposeRecordsByInteraction(ctx context.Context, interactionType string, limit int) ([]models.PurposeRecord, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, interaction_type, actor_id, purpose_justification,
		        status, policy_slug, tags, evidence, created_at
		   FROM purpose_records
		  WHERE interaction_type = $1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		interactionType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.PurposeRecord, 0)
	for rows.Next() {
		var v models.PurposeRecord
		if err := rows.Scan(&v.ID, &v.InteractionType, &v.ActorID,
			&v.PurposeJustification, &v.Status, &v.PolicySlug,
			&v.Tags, &v.Evidence, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ensure strconv linked (used in handlers via parseLimit-style helper).
var _ = strconv.Atoi
