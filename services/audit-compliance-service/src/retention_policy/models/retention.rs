use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

/// Structured selector for a retention policy. At least one of the
/// fields must match the candidate dataset / transaction for the
/// policy to apply. The `all_datasets` escape hatch is used by the
/// built-in `DELETE_ABORTED_TRANSACTIONS` system policy that targets
/// every dataset in the platform.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct RetentionSelector {
    /// Targets a specific dataset by its RID.
    pub dataset_rid: Option<String>,
    /// Targets every dataset under a project.
    pub project_id: Option<Uuid>,
    /// Targets every dataset that carries a specific marking.
    pub marking_id: Option<Uuid>,
    /// Catch-all flag. When `true`, the policy applies to every
    /// dataset in the platform regardless of the other fields.
    pub all_datasets: bool,
}

/// Structured criteria that the retention runner evaluates per
/// candidate row. All present fields are AND-combined.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct RetentionCriteria {
    /// Match transactions older than this many seconds.
    pub transaction_age_seconds: Option<i64>,
    /// Match transactions in a given state, e.g. `"ABORTED"`.
    pub transaction_state: Option<String>,
    /// Match views older than this many seconds.
    pub view_age_seconds: Option<i64>,
    /// Match resources whose `last_accessed` is older than this many
    /// seconds.
    pub last_accessed_seconds: Option<i64>,
}

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
    /// True for built-in policies seeded by migrations; surfaces the
    /// "System policy" badge in the UI.
    #[serde(default)]
    pub is_system: bool,
    #[serde(default)]
    pub selector: RetentionSelector,
    #[serde(default)]
    pub criteria: RetentionCriteria,
    /// Minutes between marking files as retired and physically
    /// deleting them. Surfaces in the policy detail modal.
    #[serde(default = "default_grace")]
    pub grace_period_minutes: i32,
    pub last_applied_at: Option<DateTime<Utc>>,
    pub next_run_at: Option<DateTime<Utc>>,
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
    #[serde(default)]
    pub selector: RetentionSelector,
    #[serde(default)]
    pub criteria: RetentionCriteria,
    #[serde(default = "default_grace")]
    pub grace_period_minutes: i32,
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
    pub selector: Option<RetentionSelector>,
    pub criteria: Option<RetentionCriteria>,
    pub grace_period_minutes: Option<i32>,
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
    #[sqlx(default)]
    pub is_system: bool,
    #[sqlx(default)]
    pub selector: Option<Value>,
    #[sqlx(default)]
    pub criteria: Option<Value>,
    #[sqlx(default)]
    pub grace_period_minutes: Option<i32>,
    #[sqlx(default)]
    pub last_applied_at: Option<DateTime<Utc>>,
    #[sqlx(default)]
    pub next_run_at: Option<DateTime<Utc>>,
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
        let selector = value
            .selector
            .map(|v| serde_json::from_value::<RetentionSelector>(v))
            .transpose()
            .map_err(|cause| cause.to_string())?
            .unwrap_or_default();
        let criteria = value
            .criteria
            .map(|v| serde_json::from_value::<RetentionCriteria>(v))
            .transpose()
            .map_err(|cause| cause.to_string())?
            .unwrap_or_default();
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
            is_system: value.is_system,
            selector,
            criteria,
            grace_period_minutes: value.grace_period_minutes.unwrap_or_else(default_grace),
            last_applied_at: value.last_applied_at,
            next_run_at: value.next_run_at,
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

fn default_grace() -> i32 {
    60
}
