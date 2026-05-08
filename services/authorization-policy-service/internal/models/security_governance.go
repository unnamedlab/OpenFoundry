package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Governance template applications ──────────────────────────────

// GovernanceTemplateApplication mirrors Rust struct of the same name.
// Records when an operator applied a named compliance template
// (HIPAA-Lite, SOC2-Read, etc.) to a particular scope (project / org).
type GovernanceTemplateApplication struct {
	ID                    uuid.UUID       `json:"id"`
	TemplateSlug          string          `json:"template_slug"`
	TemplateName          string          `json:"template_name"`
	Scope                 string          `json:"scope"`
	Standards             json.RawMessage `json:"standards"`
	PolicyNames           json.RawMessage `json:"policy_names"`
	ConstraintNames       json.RawMessage `json:"constraint_names"`
	CheckpointPrompts     json.RawMessage `json:"checkpoint_prompts"`
	DefaultReportStandard string          `json:"default_report_standard"`
	AppliedBy             string          `json:"applied_by"`
	AppliedAt             time.Time       `json:"applied_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

// ApplyGovernanceTemplateRequest is the body of POST
// /api/v1/governance-templates.
type ApplyGovernanceTemplateRequest struct {
	TemplateSlug          string          `json:"template_slug"`
	TemplateName          string          `json:"template_name"`
	Scope                 string          `json:"scope"`
	Standards             json.RawMessage `json:"standards,omitempty"`
	PolicyNames           json.RawMessage `json:"policy_names,omitempty"`
	ConstraintNames       json.RawMessage `json:"constraint_names,omitempty"`
	CheckpointPrompts     json.RawMessage `json:"checkpoint_prompts,omitempty"`
	DefaultReportStandard string          `json:"default_report_standard"`
}

// ─── Project constraints ───────────────────────────────────────────

// ProjectConstraint mirrors the Rust struct of the same name.
// One row = "project P must always carry policy X / restricted view Y / marking Z".
type ProjectConstraint struct {
	ID                          uuid.UUID       `json:"id"`
	Name                        string          `json:"name"`
	Description                 string          `json:"description"`
	Scope                       string          `json:"scope"`
	ResourceType                string          `json:"resource_type"`
	RequiredPolicyNames         json.RawMessage `json:"required_policy_names"`
	RequiredRestrictedViewNames json.RawMessage `json:"required_restricted_view_names"`
	RequiredMarkings            json.RawMessage `json:"required_markings"`
	ValidationLogic             json.RawMessage `json:"validation_logic"`
	Enabled                     bool            `json:"enabled"`
	CreatedBy                   string          `json:"created_by"`
	CreatedAt                   time.Time       `json:"created_at"`
	UpdatedAt                   time.Time       `json:"updated_at"`
}

// CreateProjectConstraintRequest is the body of POST /api/v1/project-constraints.
type CreateProjectConstraintRequest struct {
	Name                        string          `json:"name"`
	Description                 string          `json:"description,omitempty"`
	Scope                       string          `json:"scope"`
	ResourceType                string          `json:"resource_type"`
	RequiredPolicyNames         json.RawMessage `json:"required_policy_names,omitempty"`
	RequiredRestrictedViewNames json.RawMessage `json:"required_restricted_view_names,omitempty"`
	RequiredMarkings            json.RawMessage `json:"required_markings,omitempty"`
	ValidationLogic             json.RawMessage `json:"validation_logic,omitempty"`
	Enabled                     *bool           `json:"enabled,omitempty"`
}

// UpdateProjectConstraintRequest mirrors PATCH semantics.
type UpdateProjectConstraintRequest struct {
	Description                 *string         `json:"description,omitempty"`
	RequiredPolicyNames         json.RawMessage `json:"required_policy_names,omitempty"`
	RequiredRestrictedViewNames json.RawMessage `json:"required_restricted_view_names,omitempty"`
	RequiredMarkings            json.RawMessage `json:"required_markings,omitempty"`
	ValidationLogic             json.RawMessage `json:"validation_logic,omitempty"`
	Enabled                     *bool           `json:"enabled,omitempty"`
}

// ─── Structural security rules ─────────────────────────────────────

// StructuralSecurityRule mirrors Rust struct of the same name.
// Cross-resource rules (e.g. "every Iceberg table must have a marking").
type StructuralSecurityRule struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	ResourceType  string          `json:"resource_type"`
	ConditionKind string          `json:"condition_kind"`
	Config        json.RawMessage `json:"config"`
	Enabled       bool            `json:"enabled"`
	CreatedBy     string          `json:"created_by"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// CreateStructuralSecurityRuleRequest is POST /api/v1/structural-security-rules.
type CreateStructuralSecurityRuleRequest struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	ResourceType  string          `json:"resource_type"`
	ConditionKind string          `json:"condition_kind"`
	Config        json.RawMessage `json:"config,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
}

// UpdateStructuralSecurityRuleRequest mirrors PATCH semantics.
type UpdateStructuralSecurityRuleRequest struct {
	Description   *string         `json:"description,omitempty"`
	ResourceType  *string         `json:"resource_type,omitempty"`
	ConditionKind *string         `json:"condition_kind,omitempty"`
	Config        json.RawMessage `json:"config,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
}
