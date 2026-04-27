use axum::{
    Json,
    extract::{Path, State},
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::retention,
    handlers::{ServiceResult, bad_request, internal_error, not_found},
    models::retention::{
        CreateRetentionPolicyRequest, DatasetRetentionView, RetentionJob, RetentionPolicy,
        RunRetentionJobRequest, TransactionRetentionView, UpdateRetentionPolicyRequest,
    },
};

pub async fn list_policies(State(state): State<AppState>) -> ServiceResult<Vec<RetentionPolicy>> {
    let policies = retention::load_policies(&state.db)
        .await
        .map_err(internal_error)?;
    Ok(Json(policies))
}

pub async fn create_policy(
    State(state): State<AppState>,
    Json(request): Json<CreateRetentionPolicyRequest>,
) -> ServiceResult<RetentionPolicy> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    if request.target_kind.trim().is_empty() {
        return Err(bad_request("target_kind is required"));
    }
    if request.updated_by.trim().is_empty() {
        return Err(bad_request("updated_by is required"));
    }
    let rules = retention::policy_rules_payload(&request).map_err(internal_error)?;

    let row = sqlx::query_as::<_, crate::models::retention::RetentionPolicyRow>(
        "INSERT INTO retention_policies (id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
         RETURNING id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&request.name)
    .bind(&request.scope)
    .bind(&request.target_kind)
    .bind(request.retention_days)
    .bind(request.legal_hold)
    .bind(&request.purge_mode)
    .bind(rules)
    .bind(&request.updated_by)
    .bind(request.active)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| internal_error(cause.to_string()))?;

    Ok(Json(
        RetentionPolicy::try_from(row).map_err(internal_error)?,
    ))
}

pub async fn update_policy(
    State(state): State<AppState>,
    Path(policy_id): Path<Uuid>,
    Json(request): Json<UpdateRetentionPolicyRequest>,
) -> ServiceResult<RetentionPolicy> {
    let Some(mut policy) = retention::load_policy(&state.db, policy_id)
        .await
        .map_err(internal_error)?
    else {
        return Err(not_found("retention policy not found"));
    };
    retention::apply_update(&mut policy, request);
    let rules = retention::updated_rules_payload(&policy.rules).map_err(internal_error)?;

    let row = sqlx::query_as::<_, crate::models::retention::RetentionPolicyRow>(
        "UPDATE retention_policies
         SET name = $2, scope = $3, target_kind = $4, retention_days = $5, legal_hold = $6, purge_mode = $7, rules = $8::jsonb, updated_by = $9, active = $10, updated_at = NOW()
         WHERE id = $1
         RETURNING id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, created_at, updated_at",
    )
    .bind(policy_id)
    .bind(&policy.name)
    .bind(&policy.scope)
    .bind(&policy.target_kind)
    .bind(policy.retention_days)
    .bind(policy.legal_hold)
    .bind(&policy.purge_mode)
    .bind(rules)
    .bind(&policy.updated_by)
    .bind(policy.active)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| internal_error(cause.to_string()))?;

    Ok(Json(
        RetentionPolicy::try_from(row).map_err(internal_error)?,
    ))
}

pub async fn list_jobs(State(state): State<AppState>) -> ServiceResult<Vec<RetentionJob>> {
    let rows = sqlx::query_as::<_, crate::models::retention::RetentionJobRow>(
        "SELECT id, policy_id, target_dataset_id, target_transaction_id, status, action_summary, affected_record_count, created_at, completed_at
         FROM retention_jobs
         ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| internal_error(cause.to_string()))?;

    Ok(Json(rows.into_iter().map(Into::into).collect()))
}

pub async fn run_job(
    State(state): State<AppState>,
    Json(request): Json<RunRetentionJobRequest>,
) -> ServiceResult<RetentionJob> {
    let job = retention::run_job(&state.db, &request)
        .await
        .map_err(|cause| {
            if cause.contains("not found") {
                not_found(cause)
            } else {
                internal_error(cause)
            }
        })?;
    Ok(Json(job))
}

pub async fn get_dataset_retention(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> ServiceResult<DatasetRetentionView> {
    let policies = retention::load_policies(&state.db)
        .await
        .map_err(internal_error)?;
    let filtered = policies
        .into_iter()
        .filter(|policy| policy.target_kind == "dataset" || policy.scope.contains("dataset"))
        .collect::<Vec<_>>();
    Ok(Json(DatasetRetentionView {
        dataset_id,
        policies: filtered,
    }))
}

pub async fn get_transaction_retention(
    State(state): State<AppState>,
    Path(transaction_id): Path<Uuid>,
) -> ServiceResult<TransactionRetentionView> {
    let policies = retention::load_policies(&state.db)
        .await
        .map_err(internal_error)?;
    let filtered = policies
        .into_iter()
        .filter(|policy| {
            policy.target_kind == "transaction" || policy.scope.contains("transaction")
        })
        .collect::<Vec<_>>();
    Ok(Json(TransactionRetentionView {
        transaction_id,
        policies: filtered,
    }))
}
