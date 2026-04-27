use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

use crate::models::{
    compliance_report::ComplianceStandard, data_classification::ClassificationLevel, decode_json,
};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GovernanceTemplatePolicy {
    pub name: String,
    pub description: String,
    pub scope: String,
    pub classification: ClassificationLevel,
    pub required_policy_names: Vec<String>,
    pub required_restricted_view_names: Vec<String>,
    pub structural_rule_names: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GovernanceTemplate {
    pub slug: String,
    pub name: String,
    pub summary: String,
    pub standards: Vec<String>,
    pub default_report_standard: ComplianceStandard,
    pub checkpoint_prompts: Vec<String>,
    pub default_constraints: Vec<String>,
    pub policies: Vec<GovernanceTemplatePolicy>,
}

#[derive(Debug, Clone, FromRow)]
pub struct GovernanceTemplateApplicationRow {
    pub id: Uuid,
    pub template_slug: String,
    pub template_name: String,
    pub scope: String,
    pub standards: Value,
    pub policy_names: Value,
    pub constraint_names: Value,
    pub checkpoint_prompts: Value,
    pub default_report_standard: String,
    pub applied_by: String,
    pub applied_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GovernanceTemplateApplication {
    pub id: Uuid,
    pub template_slug: String,
    pub template_name: String,
    pub scope: String,
    pub standards: Vec<String>,
    pub policy_names: Vec<String>,
    pub constraint_names: Vec<String>,
    pub checkpoint_prompts: Vec<String>,
    pub default_report_standard: ComplianceStandard,
    pub applied_by: String,
    pub applied_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl TryFrom<GovernanceTemplateApplicationRow> for GovernanceTemplateApplication {
    type Error = String;

    fn try_from(row: GovernanceTemplateApplicationRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: row.id,
            template_slug: row.template_slug,
            template_name: row.template_name,
            scope: row.scope,
            standards: decode_json(row.standards, "standards")?,
            policy_names: decode_json(row.policy_names, "policy_names")?,
            constraint_names: decode_json(row.constraint_names, "constraint_names")?,
            checkpoint_prompts: decode_json(row.checkpoint_prompts, "checkpoint_prompts")?,
            default_report_standard: row.default_report_standard.parse()?,
            applied_by: row.applied_by,
            applied_at: row.applied_at,
            updated_at: row.updated_at,
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct ProjectConstraint {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub scope: String,
    pub resource_type: String,
    pub required_policy_names: Value,
    pub required_restricted_view_names: Value,
    pub required_markings: Value,
    pub validation_logic: Value,
    pub enabled: bool,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct StructuralSecurityRule {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub resource_type: String,
    pub condition_kind: String,
    pub config: Value,
    pub enabled: bool,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CompliancePostureOverview {
    pub standards: Vec<CompliancePostureStandard>,
    pub supported_capabilities: Vec<String>,
    pub active_template_application_count: i64,
    pub active_constraint_count: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CompliancePostureStandard {
    pub standard: ComplianceStandard,
    pub template_available: bool,
    pub applied_scope_count: i64,
    pub latest_report_status: Option<String>,
    pub latest_report_generated_at: Option<DateTime<Utc>>,
    pub structural_rule_count: i64,
    pub coverage_score: i32,
    pub evidence_summary: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IntegrityCheckIssue {
    pub severity: String,
    pub code: String,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IntegrityValidationResponse {
    pub scope: Option<String>,
    pub resource_type: Option<String>,
    pub valid: bool,
    pub issues: Vec<IntegrityCheckIssue>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ApplyGovernanceTemplateRequest {
    pub scope: Option<String>,
    pub updated_by: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateProjectConstraintRequest {
    pub name: String,
    #[serde(default)]
    pub description: String,
    pub scope: String,
    pub resource_type: String,
    #[serde(default)]
    pub required_policy_names: Vec<String>,
    #[serde(default)]
    pub required_restricted_view_names: Vec<String>,
    #[serde(default)]
    pub required_markings: Vec<String>,
    #[serde(default)]
    pub validation_logic: Value,
    #[serde(default = "default_enabled")]
    pub enabled: bool,
    pub created_by: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateProjectConstraintRequest {
    pub description: Option<String>,
    pub scope: Option<String>,
    pub resource_type: Option<String>,
    pub required_policy_names: Option<Vec<String>>,
    pub required_restricted_view_names: Option<Vec<String>>,
    pub required_markings: Option<Vec<String>>,
    pub validation_logic: Option<Value>,
    pub enabled: Option<bool>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateStructuralSecurityRuleRequest {
    pub name: String,
    #[serde(default)]
    pub description: String,
    pub resource_type: String,
    pub condition_kind: String,
    #[serde(default)]
    pub config: Value,
    #[serde(default = "default_enabled")]
    pub enabled: bool,
    pub created_by: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateStructuralSecurityRuleRequest {
    pub description: Option<String>,
    pub resource_type: Option<String>,
    pub condition_kind: Option<String>,
    pub config: Option<Value>,
    pub enabled: Option<bool>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct IntegrityValidationRequest {
    pub scope: Option<String>,
    pub resource_type: Option<String>,
    #[serde(default)]
    pub policy_names: Vec<String>,
    #[serde(default)]
    pub restricted_view_names: Vec<String>,
    #[serde(default)]
    pub markings: Vec<String>,
}

fn default_enabled() -> bool {
    true
}
