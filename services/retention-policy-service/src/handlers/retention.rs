use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
};
use serde::Deserialize;
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

/// Query string for `GET /v1/policies`.
///
/// All fields are optional and AND-combined. The catalog/UI uses
/// `dataset_rid` to fetch the policies that apply to a single dataset
/// (T4.4 Retention tab); the deletion runner uses `active=true` to
/// enumerate work to do.
#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct ListPoliciesQuery {
    /// Restrict to policies whose selector matches this dataset RID
    /// (direct match, project membership, or `all_datasets=true`).
    pub dataset_rid: Option<String>,
    pub project_id: Option<Uuid>,
    pub marking_id: Option<Uuid>,
    pub active: Option<bool>,
    pub system_only: Option<bool>,
}

pub async fn list_policies(
    State(state): State<AppState>,
    Query(query): Query<ListPoliciesQuery>,
) -> ServiceResult<Vec<RetentionPolicy>> {
    let policies = retention::load_policies(&state.db)
        .await
        .map_err(internal_error)?;
    Ok(Json(retention::filter_policies(policies, &query)))
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
    let selector =
        serde_json::to_value(&request.selector).map_err(|e| internal_error(e.to_string()))?;
    let criteria =
        serde_json::to_value(&request.criteria).map_err(|e| internal_error(e.to_string()))?;

    let row = sqlx::query_as::<_, crate::models::retention::RetentionPolicyRow>(
        "INSERT INTO retention_policies (id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, selector, criteria, grace_period_minutes)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11::jsonb, $12::jsonb, $13)
         RETURNING id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, is_system, selector, criteria, grace_period_minutes, last_applied_at, next_run_at, created_at, updated_at",
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
    .bind(selector)
    .bind(criteria)
    .bind(request.grace_period_minutes)
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
    let selector =
        serde_json::to_value(&policy.selector).map_err(|e| internal_error(e.to_string()))?;
    let criteria =
        serde_json::to_value(&policy.criteria).map_err(|e| internal_error(e.to_string()))?;

    let row = sqlx::query_as::<_, crate::models::retention::RetentionPolicyRow>(
        "UPDATE retention_policies
         SET name = $2, scope = $3, target_kind = $4, retention_days = $5, legal_hold = $6, purge_mode = $7, rules = $8::jsonb, updated_by = $9, active = $10, selector = $11::jsonb, criteria = $12::jsonb, grace_period_minutes = $13, updated_at = NOW()
         WHERE id = $1
         RETURNING id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, is_system, selector, criteria, grace_period_minutes, last_applied_at, next_run_at, created_at, updated_at",
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
    .bind(selector)
    .bind(criteria)
    .bind(policy.grace_period_minutes)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| internal_error(cause.to_string()))?;

    Ok(Json(
        RetentionPolicy::try_from(row).map_err(internal_error)?,
    ))
}

/// `GET /v1/policies/{id}`.
pub async fn get_policy(
    State(state): State<AppState>,
    Path(policy_id): Path<Uuid>,
) -> ServiceResult<RetentionPolicy> {
    match retention::load_policy(&state.db, policy_id)
        .await
        .map_err(internal_error)?
    {
        Some(policy) => Ok(Json(policy)),
        None => Err(not_found("retention policy not found")),
    }
}

/// `DELETE /v1/policies/{id}`. System policies (`is_system = true`)
/// can never be deleted: returns 409 Conflict.
pub async fn delete_policy(
    State(state): State<AppState>,
    Path(policy_id): Path<Uuid>,
) -> Result<StatusCode, (StatusCode, Json<crate::handlers::ErrorResponse>)> {
    let Some(existing) = retention::load_policy(&state.db, policy_id)
        .await
        .map_err(internal_error)?
    else {
        return Err(not_found("retention policy not found"));
    };
    if existing.is_system {
        return Err((
            StatusCode::CONFLICT,
            Json(crate::handlers::ErrorResponse {
                error: "system policies cannot be deleted".into(),
            }),
        ));
    }
    sqlx::query("DELETE FROM retention_policies WHERE id = $1")
        .bind(policy_id)
        .execute(&state.db)
        .await
        .map_err(|cause| internal_error(cause.to_string()))?;
    Ok(StatusCode::NO_CONTENT)
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

// ─────────────────────────────────────────────────────────────────────────────
// P4 — applicable-policies + retention-preview.
//
// `applicable-policies` resolves the inheritance chain (Org → Space →
// Project → Dataset) for a dataset RID and surfaces the winning
// "most restrictive" policy. `retention-preview` simulates which
// transactions / files would be purged if the runner fired now (or in
// `as_of_days` days). Both share the same context query params so the
// UI can pass the same dataset metadata once.
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct ResolutionContext {
    pub project_id: Option<Uuid>,
    pub marking_id: Option<Uuid>,
    pub space_id: Option<Uuid>,
    pub org_id: Option<Uuid>,
}

#[derive(Debug, Default, serde::Serialize)]
pub struct InheritedPolicies {
    pub org: Vec<crate::models::retention::RetentionPolicy>,
    pub space: Vec<crate::models::retention::RetentionPolicy>,
    pub project: Vec<crate::models::retention::RetentionPolicy>,
}

#[derive(Debug, serde::Serialize)]
pub struct PolicyConflict {
    pub winner_id: Uuid,
    pub loser_id: Uuid,
    pub reason: &'static str,
}

#[derive(Debug, serde::Serialize)]
pub struct ApplicablePoliciesResponse {
    pub dataset_rid: String,
    pub context: ResolutionContext,
    pub inherited: InheritedPolicies,
    pub explicit: Vec<crate::models::retention::RetentionPolicy>,
    pub effective: Option<crate::models::retention::RetentionPolicy>,
    pub conflicts: Vec<PolicyConflict>,
}

impl serde::Serialize for ResolutionContext {
    fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        use serde::ser::SerializeStruct;
        let mut st = s.serialize_struct("ResolutionContext", 4)?;
        st.serialize_field("project_id", &self.project_id)?;
        st.serialize_field("marking_id", &self.marking_id)?;
        st.serialize_field("space_id", &self.space_id)?;
        st.serialize_field("org_id", &self.org_id)?;
        st.end()
    }
}

pub async fn applicable_policies(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(ctx): Query<ResolutionContext>,
) -> ServiceResult<ApplicablePoliciesResponse> {
    let policies = retention::load_policies(&state.db)
        .await
        .map_err(internal_error)?;

    let resolved = retention::resolve_applicable(&policies, &rid, &ctx);

    // Tag the metric with the dominant inheritance origin so dashboards
    // show whether explicit policies are common.
    let origin = if !resolved.explicit.is_empty() {
        "hit_dataset"
    } else if !resolved.inherited.project.is_empty() {
        "hit_project"
    } else if !resolved.inherited.space.is_empty() {
        "hit_space"
    } else if !resolved.inherited.org.is_empty() {
        "hit_org"
    } else {
        "none"
    };
    crate::metrics::RETENTION_APPLICABLE_TOTAL
        .with_label_values(&[origin])
        .inc();

    Ok(Json(ApplicablePoliciesResponse {
        dataset_rid: rid,
        context: ctx,
        inherited: resolved.inherited,
        explicit: resolved.explicit,
        effective: resolved.effective,
        conflicts: resolved.conflicts,
    }))
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct RetentionPreviewQuery {
    /// `now + as_of_days` is the wall-clock the simulator pretends it's
    /// running at. Negative values are clamped to 0.
    pub as_of_days: Option<i64>,
    pub project_id: Option<Uuid>,
    pub marking_id: Option<Uuid>,
    pub space_id: Option<Uuid>,
    pub org_id: Option<Uuid>,
}

#[derive(Debug, serde::Serialize)]
pub struct RetentionPreviewTransaction {
    pub id: Uuid,
    pub tx_type: String,
    pub status: String,
    pub started_at: chrono::DateTime<chrono::Utc>,
    pub committed_at: Option<chrono::DateTime<chrono::Utc>>,
    pub would_delete: bool,
    pub policy_id: Option<Uuid>,
    pub policy_name: Option<String>,
    pub reason: Option<String>,
}

#[derive(Debug, serde::Serialize)]
pub struct RetentionPreviewFile {
    pub id: Uuid,
    pub transaction_id: Uuid,
    pub logical_path: String,
    pub physical_uri: String,
    pub size_bytes: i64,
    pub policy_id: Uuid,
    pub policy_name: String,
    pub reason: String,
}

#[derive(Debug, serde::Serialize, Default)]
pub struct RetentionPreviewSummary {
    pub transactions_total: usize,
    pub transactions_would_delete: usize,
    pub files_total: usize,
    pub bytes_total: i64,
}

#[derive(Debug, serde::Serialize)]
pub struct RetentionPreviewResponse {
    pub dataset_rid: String,
    pub as_of_days: i64,
    pub as_of: chrono::DateTime<chrono::Utc>,
    pub effective_policy: Option<crate::models::retention::RetentionPolicy>,
    pub transactions: Vec<RetentionPreviewTransaction>,
    pub files: Vec<RetentionPreviewFile>,
    pub summary: RetentionPreviewSummary,
    /// Non-fatal warnings. Surfaces e.g. "dataset_transactions table not
    /// found" when DVS migrations haven't been applied to this DB.
    pub warnings: Vec<String>,
}

pub async fn retention_preview(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(q): Query<RetentionPreviewQuery>,
) -> ServiceResult<RetentionPreviewResponse> {
    let as_of_days = q.as_of_days.unwrap_or(0).max(0);
    let bucket = crate::metrics::preview_bucket(as_of_days);
    crate::metrics::RETENTION_PREVIEW_TOTAL
        .with_label_values(&[bucket])
        .inc();

    let policies = retention::load_policies(&state.db)
        .await
        .map_err(internal_error)?;
    let ctx = ResolutionContext {
        project_id: q.project_id,
        marking_id: q.marking_id,
        space_id: q.space_id,
        org_id: q.org_id,
    };
    let resolved = retention::resolve_applicable(&policies, &rid, &ctx);

    let preview = retention::run_preview(&state.db, &rid, as_of_days, &resolved)
        .await
        .map_err(internal_error)?;

    Ok(Json(preview))
}
