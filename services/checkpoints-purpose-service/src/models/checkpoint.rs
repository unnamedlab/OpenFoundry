use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum InteractionSensitivity {
    Normal,
    High,
    Critical,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointPolicyRule {
    pub key: String,
    pub expected: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointPolicy {
    pub slug: String,
    pub name: String,
    pub interaction_type: String,
    pub sensitivity: InteractionSensitivity,
    pub enforcement_mode: String,
    pub prompts: Vec<String>,
    pub rules: Vec<CheckpointPolicyRule>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SensitiveInteractionConfig {
    pub interaction_type: String,
    pub sensitivity: InteractionSensitivity,
    pub require_purpose_justification: bool,
    pub require_auditable_record: bool,
    pub linked_policy_slug: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EvaluateCheckpointRequest {
    pub interaction_type: String,
    pub actor_id: Option<Uuid>,
    #[serde(default)]
    pub purpose_justification: Option<String>,
    #[serde(default)]
    pub requested_private_network: bool,
    #[serde(default)]
    pub requires_approval: bool,
    #[serde(default)]
    pub tags: Vec<String>,
    #[serde(default)]
    pub evidence: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointEvaluation {
    pub record_id: Uuid,
    pub approved: bool,
    pub status: String,
    pub required_prompts: Vec<String>,
    pub policy_slug: Option<String>,
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreatePolicyRequest {
    pub slug: String,
    pub name: String,
    pub interaction_type: String,
    pub sensitivity: InteractionSensitivity,
    pub enforcement_mode: String,
    #[serde(default)]
    pub prompts: Vec<String>,
    #[serde(default)]
    pub rules: Vec<CheckpointPolicyRule>,
}

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct PolicyRow {
    pub slug: String,
    pub name: String,
    pub interaction_type: String,
    pub sensitivity: String,
    pub enforcement_mode: String,
    pub prompts: Value,
    pub rules: Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl TryFrom<PolicyRow> for CheckpointPolicy {
    type Error = String;

    fn try_from(row: PolicyRow) -> Result<Self, Self::Error> {
        let _ = row.created_at;
        let _ = row.updated_at;
        Ok(Self {
            slug: row.slug,
            name: row.name,
            interaction_type: row.interaction_type,
            sensitivity: match row.sensitivity.as_str() {
                "critical" => InteractionSensitivity::Critical,
                "high" => InteractionSensitivity::High,
                _ => InteractionSensitivity::Normal,
            },
            enforcement_mode: row.enforcement_mode,
            prompts: serde_json::from_value(row.prompts).map_err(|cause| cause.to_string())?,
            rules: serde_json::from_value(row.rules).map_err(|cause| cause.to_string())?,
        })
    }
}

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct SensitiveConfigRow {
    pub interaction_type: String,
    pub sensitivity: String,
    pub require_purpose_justification: bool,
    pub require_auditable_record: bool,
    pub linked_policy_slug: Option<String>,
}

impl From<SensitiveConfigRow> for SensitiveInteractionConfig {
    fn from(value: SensitiveConfigRow) -> Self {
        Self {
            interaction_type: value.interaction_type,
            sensitivity: match value.sensitivity.as_str() {
                "critical" => InteractionSensitivity::Critical,
                "high" => InteractionSensitivity::High,
                _ => InteractionSensitivity::Normal,
            },
            require_purpose_justification: value.require_purpose_justification,
            require_auditable_record: value.require_auditable_record,
            linked_policy_slug: value.linked_policy_slug,
        }
    }
}
