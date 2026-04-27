use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PurposeTemplate {
    pub slug: String,
    pub name: String,
    pub summary: String,
    pub prompts: Vec<String>,
    pub required_tags: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PurposeRecord {
    pub id: Uuid,
    pub interaction_type: String,
    pub actor_id: Option<Uuid>,
    pub purpose_justification: Option<String>,
    pub status: String,
    pub policy_slug: Option<String>,
    pub tags: Vec<String>,
    pub evidence: Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ListRecordsQuery {
    pub interaction_type: Option<String>,
    pub actor_id: Option<Uuid>,
}

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct PurposeRecordRow {
    pub id: Uuid,
    pub interaction_type: String,
    pub actor_id: Option<Uuid>,
    pub purpose_justification: Option<String>,
    pub status: String,
    pub policy_slug: Option<String>,
    pub tags: Value,
    pub evidence: Value,
    pub created_at: DateTime<Utc>,
}

impl TryFrom<PurposeRecordRow> for PurposeRecord {
    type Error = String;

    fn try_from(value: PurposeRecordRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: value.id,
            interaction_type: value.interaction_type,
            actor_id: value.actor_id,
            purpose_justification: value.purpose_justification,
            status: value.status,
            policy_slug: value.policy_slug,
            tags: serde_json::from_value(value.tags).map_err(|cause| cause.to_string())?,
            evidence: value.evidence,
            created_at: value.created_at,
        })
    }
}
