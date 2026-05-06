package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── Governance template applications ──────────────────────────────

const gtaSelect = `SELECT id, template_slug, template_name, scope, standards,
	policy_names, constraint_names, checkpoint_prompts, default_report_standard,
	applied_by, applied_at, updated_at FROM governance_template_applications`

func (r *Repo) ListGovernanceTemplateApplications(ctx context.Context) ([]models.GovernanceTemplateApplication, error) {
	rows, err := r.Pool.Query(ctx, gtaSelect+` ORDER BY applied_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.GovernanceTemplateApplication, 0)
	for rows.Next() {
		v, err := scanGovernanceApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// ApplyGovernanceTemplate is an upsert keyed by (template_slug, scope) —
// idempotent application of a compliance template. Updates the row in
// place when (slug, scope) collides.
func (r *Repo) ApplyGovernanceTemplate(ctx context.Context, body *models.ApplyGovernanceTemplateRequest, appliedBy string) (*models.GovernanceTemplateApplication, error) {
	id := uuid.New()
	now := time.Now().UTC()
	stds := defaultJSON(body.Standards, "[]")
	pol := defaultJSON(body.PolicyNames, "[]")
	cst := defaultJSON(body.ConstraintNames, "[]")
	cp := defaultJSON(body.CheckpointPrompts, "[]")
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO governance_template_applications
		    (id, template_slug, template_name, scope, standards,
		     policy_names, constraint_names, checkpoint_prompts,
		     default_report_standard, applied_by, applied_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		 ON CONFLICT (template_slug, scope) DO UPDATE SET
		    template_name = EXCLUDED.template_name,
		    standards = EXCLUDED.standards,
		    policy_names = EXCLUDED.policy_names,
		    constraint_names = EXCLUDED.constraint_names,
		    checkpoint_prompts = EXCLUDED.checkpoint_prompts,
		    default_report_standard = EXCLUDED.default_report_standard,
		    applied_by = EXCLUDED.applied_by,
		    updated_at = EXCLUDED.updated_at
		 RETURNING id, template_slug, template_name, scope, standards,
		           policy_names, constraint_names, checkpoint_prompts,
		           default_report_standard, applied_by, applied_at, updated_at`,
		id, body.TemplateSlug, body.TemplateName, body.Scope, stds, pol, cst, cp,
		body.DefaultReportStandard, appliedBy, now,
	)
	return scanGovernanceApp(row)
}

func (r *Repo) DeleteGovernanceTemplateApplication(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM governance_template_applications WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanGovernanceApp(r rowLikeT) (*models.GovernanceTemplateApplication, error) {
	v := &models.GovernanceTemplateApplication{}
	if err := r.Scan(&v.ID, &v.TemplateSlug, &v.TemplateName, &v.Scope,
		&v.Standards, &v.PolicyNames, &v.ConstraintNames, &v.CheckpointPrompts,
		&v.DefaultReportStandard, &v.AppliedBy, &v.AppliedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── Project constraints ───────────────────────────────────────────

const pcSelect = `SELECT id, name, description, scope, resource_type,
	required_policy_names, required_restricted_view_names, required_markings,
	validation_logic, enabled, created_by, created_at, updated_at
	FROM project_constraints`

func (r *Repo) ListProjectConstraints(ctx context.Context) ([]models.ProjectConstraint, error) {
	rows, err := r.Pool.Query(ctx, pcSelect+` ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ProjectConstraint, 0)
	for rows.Next() {
		v, err := scanProjectConstraint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) CreateProjectConstraint(ctx context.Context, body *models.CreateProjectConstraintRequest, createdBy string) (*models.ProjectConstraint, error) {
	id := uuid.New()
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO project_constraints
		    (id, name, description, scope, resource_type,
		     required_policy_names, required_restricted_view_names, required_markings,
		     validation_logic, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, name, description, scope, resource_type,
		           required_policy_names, required_restricted_view_names, required_markings,
		           validation_logic, enabled, created_by, created_at, updated_at`,
		id, body.Name, body.Description, body.Scope, body.ResourceType,
		defaultJSON(body.RequiredPolicyNames, "[]"),
		defaultJSON(body.RequiredRestrictedViewNames, "[]"),
		defaultJSON(body.RequiredMarkings, "[]"),
		defaultJSON(body.ValidationLogic, "{}"),
		enabled, createdBy,
	)
	return scanProjectConstraint(row)
}

func (r *Repo) UpdateProjectConstraint(ctx context.Context, id uuid.UUID, body *models.UpdateProjectConstraintRequest) (*models.ProjectConstraint, error) {
	current, err := r.GetProjectConstraint(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	desc := current.Description
	if body.Description != nil {
		desc = *body.Description
	}
	rpn := current.RequiredPolicyNames
	if len(body.RequiredPolicyNames) > 0 {
		rpn = body.RequiredPolicyNames
	}
	rrvn := current.RequiredRestrictedViewNames
	if len(body.RequiredRestrictedViewNames) > 0 {
		rrvn = body.RequiredRestrictedViewNames
	}
	rm := current.RequiredMarkings
	if len(body.RequiredMarkings) > 0 {
		rm = body.RequiredMarkings
	}
	vl := current.ValidationLogic
	if len(body.ValidationLogic) > 0 {
		vl = body.ValidationLogic
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE project_constraints SET
		    description = $2, required_policy_names = $3,
		    required_restricted_view_names = $4, required_markings = $5,
		    validation_logic = $6, enabled = $7, updated_at = $8
		  WHERE id = $1
		  RETURNING id, name, description, scope, resource_type,
		            required_policy_names, required_restricted_view_names, required_markings,
		            validation_logic, enabled, created_by, created_at, updated_at`,
		id, desc, rpn, rrvn, rm, vl, enabled, time.Now().UTC(),
	)
	return scanProjectConstraint(row)
}

func (r *Repo) GetProjectConstraint(ctx context.Context, id uuid.UUID) (*models.ProjectConstraint, error) {
	row := r.Pool.QueryRow(ctx, pcSelect+` WHERE id = $1`, id)
	v, err := scanProjectConstraint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) DeleteProjectConstraint(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM project_constraints WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanProjectConstraint(r rowLikeT) (*models.ProjectConstraint, error) {
	v := &models.ProjectConstraint{}
	if err := r.Scan(&v.ID, &v.Name, &v.Description, &v.Scope, &v.ResourceType,
		&v.RequiredPolicyNames, &v.RequiredRestrictedViewNames, &v.RequiredMarkings,
		&v.ValidationLogic, &v.Enabled, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── Structural security rules ─────────────────────────────────────

const ssrSelect = `SELECT id, name, description, resource_type, condition_kind,
	config, enabled, created_by, created_at, updated_at
	FROM structural_security_rules`

func (r *Repo) ListStructuralSecurityRules(ctx context.Context) ([]models.StructuralSecurityRule, error) {
	rows, err := r.Pool.Query(ctx, ssrSelect+` ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.StructuralSecurityRule, 0)
	for rows.Next() {
		v, err := scanStructuralRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) CreateStructuralSecurityRule(ctx context.Context, body *models.CreateStructuralSecurityRuleRequest, createdBy string) (*models.StructuralSecurityRule, error) {
	id := uuid.New()
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO structural_security_rules
		    (id, name, description, resource_type, condition_kind, config, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, name, description, resource_type, condition_kind,
		           config, enabled, created_by, created_at, updated_at`,
		id, body.Name, body.Description, body.ResourceType, body.ConditionKind,
		defaultJSON(body.Config, "{}"), enabled, createdBy,
	)
	return scanStructuralRule(row)
}

func (r *Repo) UpdateStructuralSecurityRule(ctx context.Context, id uuid.UUID, body *models.UpdateStructuralSecurityRuleRequest) (*models.StructuralSecurityRule, error) {
	current, err := r.GetStructuralSecurityRule(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	desc := current.Description
	if body.Description != nil {
		desc = *body.Description
	}
	rt := current.ResourceType
	if body.ResourceType != nil {
		rt = *body.ResourceType
	}
	ck := current.ConditionKind
	if body.ConditionKind != nil {
		ck = *body.ConditionKind
	}
	cfg := current.Config
	if len(body.Config) > 0 {
		cfg = body.Config
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE structural_security_rules SET
		    description = $2, resource_type = $3, condition_kind = $4,
		    config = $5, enabled = $6, updated_at = $7
		  WHERE id = $1
		  RETURNING id, name, description, resource_type, condition_kind,
		            config, enabled, created_by, created_at, updated_at`,
		id, desc, rt, ck, cfg, enabled, time.Now().UTC(),
	)
	return scanStructuralRule(row)
}

func (r *Repo) GetStructuralSecurityRule(ctx context.Context, id uuid.UUID) (*models.StructuralSecurityRule, error) {
	row := r.Pool.QueryRow(ctx, ssrSelect+` WHERE id = $1`, id)
	v, err := scanStructuralRule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) DeleteStructuralSecurityRule(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM structural_security_rules WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanStructuralRule(r rowLikeT) (*models.StructuralSecurityRule, error) {
	v := &models.StructuralSecurityRule{}
	if err := r.Scan(&v.ID, &v.Name, &v.Description, &v.ResourceType,
		&v.ConditionKind, &v.Config, &v.Enabled, &v.CreatedBy,
		&v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── helpers ───────────────────────────────────────────────────────

// defaultJSON returns `body` when non-empty, or a fallback rendered as
// json.RawMessage. Avoids passing nil to JSONB columns.
func defaultJSON(body json.RawMessage, fallback string) json.RawMessage {
	if len(body) > 0 {
		return body
	}
	return json.RawMessage(fallback)
}

// ensure fmt is referenced — formatters land in tests / future expansion.
var _ = fmt.Sprintf
