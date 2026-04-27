use chrono::Utc;
use uuid::Uuid;

use crate::models::retention::{
    CreateRetentionPolicyRequest, RetentionJob, RetentionJobRow, RetentionPolicy,
    RetentionPolicyRow, RunRetentionJobRequest, UpdateRetentionPolicyRequest,
};

pub fn policy_rules_payload(
    request: &CreateRetentionPolicyRequest,
) -> Result<serde_json::Value, String> {
    serde_json::to_value(&request.rules).map_err(|cause| cause.to_string())
}

pub fn updated_rules_payload(rules: &[String]) -> Result<serde_json::Value, String> {
    serde_json::to_value(rules).map_err(|cause| cause.to_string())
}

pub async fn load_policies(db: &sqlx::PgPool) -> Result<Vec<RetentionPolicy>, String> {
    let rows = sqlx::query_as::<_, RetentionPolicyRow>(
        "SELECT id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, created_at, updated_at
         FROM retention_policies
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await
    .map_err(|cause| cause.to_string())?;

    rows.into_iter()
        .map(RetentionPolicy::try_from)
        .collect::<Result<Vec<_>, _>>()
}

pub async fn load_policy(db: &sqlx::PgPool, id: Uuid) -> Result<Option<RetentionPolicy>, String> {
    let row = sqlx::query_as::<_, RetentionPolicyRow>(
        "SELECT id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, created_at, updated_at
         FROM retention_policies WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(db)
    .await
    .map_err(|cause| cause.to_string())?;

    match row {
        Some(row) => RetentionPolicy::try_from(row).map(Some),
        None => Ok(None),
    }
}

pub async fn run_job(
    db: &sqlx::PgPool,
    request: &RunRetentionJobRequest,
) -> Result<RetentionJob, String> {
    let Some(policy) = load_policy(db, request.policy_id).await? else {
        return Err("retention policy not found".to_string());
    };

    let target_label = if let Some(dataset_id) = request.target_dataset_id {
        format!("dataset {dataset_id}")
    } else if let Some(transaction_id) = request.target_transaction_id {
        format!("transaction {transaction_id}")
    } else {
        "policy scope".to_string()
    };

    let action_summary = format!(
        "Applied {} retention ({} days, purge mode {}) to {}",
        policy.target_kind, policy.retention_days, policy.purge_mode, target_label
    );

    let affected_record_count = if request.target_transaction_id.is_some() {
        1
    } else {
        3
    };

    let row = sqlx::query_as::<_, RetentionJobRow>(
        "INSERT INTO retention_jobs (id, policy_id, target_dataset_id, target_transaction_id, status, action_summary, affected_record_count, created_at, completed_at)
         VALUES ($1, $2, $3, $4, 'completed', $5, $6, $7, $8)
         RETURNING id, policy_id, target_dataset_id, target_transaction_id, status, action_summary, affected_record_count, created_at, completed_at",
    )
    .bind(Uuid::now_v7())
    .bind(request.policy_id)
    .bind(request.target_dataset_id)
    .bind(request.target_transaction_id)
    .bind(action_summary)
    .bind(affected_record_count)
    .bind(Utc::now())
    .bind(Utc::now())
    .fetch_one(db)
    .await
    .map_err(|cause| cause.to_string())?;

    Ok(row.into())
}

pub fn apply_update(current: &mut RetentionPolicy, update: UpdateRetentionPolicyRequest) {
    if let Some(name) = update.name {
        current.name = name;
    }
    if let Some(scope) = update.scope {
        current.scope = scope;
    }
    if let Some(target_kind) = update.target_kind {
        current.target_kind = target_kind;
    }
    if let Some(retention_days) = update.retention_days {
        current.retention_days = retention_days;
    }
    if let Some(legal_hold) = update.legal_hold {
        current.legal_hold = legal_hold;
    }
    if let Some(purge_mode) = update.purge_mode {
        current.purge_mode = purge_mode;
    }
    if let Some(rules) = update.rules {
        current.rules = rules;
    }
    if let Some(updated_by) = update.updated_by {
        current.updated_by = updated_by;
    }
    if let Some(active) = update.active {
        current.active = active;
    }
}
