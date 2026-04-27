use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RetentionPolicy {
    pub id: Uuid,
    pub name: String,
    pub scope: String,
    pub target_kind: String,
    pub retention_days: i32,
    pub legal_hold: bool,
    pub purge_mode: String,
    pub rules: Vec<String>,
    pub updated_by: String,
    pub active: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateRetentionPolicyRequest {
    pub name: String,
    #[serde(default)]
    pub scope: String,
    pub target_kind: String,
    pub retention_days: i32,
    #[serde(default)]
    pub legal_hold: bool,
    pub purge_mode: String,
    #[serde(default)]
    pub rules: Vec<String>,
    pub updated_by: String,
    #[serde(default = "default_true")]
    pub active: bool,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateRetentionPolicyRequest {
    pub name: Option<String>,
    pub scope: Option<String>,
    pub target_kind: Option<String>,
    pub retention_days: Option<i32>,
    pub legal_hold: Option<bool>,
    pub purge_mode: Option<String>,
    pub rules: Option<Vec<String>>,
    pub updated_by: Option<String>,
    pub active: Option<bool>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RetentionJob {
    pub id: Uuid,
    pub policy_id: Uuid,
    pub target_dataset_id: Option<Uuid>,
    pub target_transaction_id: Option<Uuid>,
    pub status: String,
    pub action_summary: String,
    pub affected_record_count: i32,
    pub created_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RunRetentionJobRequest {
    pub policy_id: Uuid,
    pub target_dataset_id: Option<Uuid>,
    pub target_transaction_id: Option<Uuid>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetRetentionView {
    pub dataset_id: Uuid,
    pub policies: Vec<RetentionPolicy>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransactionRetentionView {
    pub transaction_id: Uuid,
    pub policies: Vec<RetentionPolicy>,
}

#[derive(Debug, Clone, FromRow)]
pub struct RetentionPolicyRow {
    pub id: Uuid,
    pub name: String,
    pub scope: String,
    pub target_kind: String,
    pub retention_days: i32,
    pub legal_hold: bool,
    pub purge_mode: String,
    pub rules: Value,
    pub updated_by: String,
    pub active: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow)]
pub struct RetentionJobRow {
    pub id: Uuid,
    pub policy_id: Uuid,
    pub target_dataset_id: Option<Uuid>,
    pub target_transaction_id: Option<Uuid>,
    pub status: String,
    pub action_summary: String,
    pub affected_record_count: i32,
    pub created_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
}

impl TryFrom<RetentionPolicyRow> for RetentionPolicy {
    type Error = String;

    fn try_from(value: RetentionPolicyRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: value.id,
            name: value.name,
            scope: value.scope,
            target_kind: value.target_kind,
            retention_days: value.retention_days,
            legal_hold: value.legal_hold,
            purge_mode: value.purge_mode,
            rules: serde_json::from_value(value.rules).map_err(|cause| cause.to_string())?,
            updated_by: value.updated_by,
            active: value.active,
            created_at: value.created_at,
            updated_at: value.updated_at,
        })
    }
}

impl From<RetentionJobRow> for RetentionJob {
    fn from(value: RetentionJobRow) -> Self {
        Self {
            id: value.id,
            policy_id: value.policy_id,
            target_dataset_id: value.target_dataset_id,
            target_transaction_id: value.target_transaction_id,
            status: value.status,
            action_summary: value.action_summary,
            affected_record_count: value.affected_record_count,
            created_at: value.created_at,
            completed_at: value.completed_at,
        }
    }
}

fn default_true() -> bool {
    true
}
