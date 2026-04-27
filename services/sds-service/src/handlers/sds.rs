use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::sds,
    models::sensitive_data::{
        CreateRemediationRuleRequest, MarkSensitiveIssueRequest, RemediationRule,
        RunSensitiveDataScanRequest, SensitiveDataIssue, SensitiveDataIssueRow,
        SensitiveDataScanJob, SensitiveDataScanJobRow, SensitiveDataScanRequest,
        SensitiveDataScanResponse,
    },
};

use super::{ServiceResult, bad_request, db_error, internal_error};

pub async fn scan_sensitive_data(
    Json(request): Json<SensitiveDataScanRequest>,
) -> ServiceResult<SensitiveDataScanResponse> {
    if request.content.trim().is_empty() {
        return Err(bad_request("content is required"));
    }
    Ok(Json(sds::scan(&request)))
}

pub async fn run_scan_job(
    State(state): State<AppState>,
    Json(request): Json<RunSensitiveDataScanRequest>,
) -> ServiceResult<SensitiveDataScanJob> {
    if request.target_name.trim().is_empty() {
        return Err(bad_request("target_name is required"));
    }
    if request.content.trim().is_empty() {
        return Err(bad_request("content is required"));
    }
    let job = sds::create_scan_job(&state.db, &request)
        .await
        .map_err(internal_error)?;
    Ok(Json(job))
}

pub async fn list_scan_jobs(
    State(state): State<AppState>,
) -> ServiceResult<Vec<SensitiveDataScanJob>> {
    let rows = sqlx::query_as::<_, SensitiveDataScanJobRow>(
        "SELECT id, target_name, scope, status, risk_score, findings, issue_count, redacted_content, remediations, requested_by, created_at, updated_at
         FROM sds_scan_jobs
         ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let mut jobs = Vec::new();
    for row in rows {
        jobs.push(sds::job_from_row(row).map_err(internal_error)?);
    }
    Ok(Json(jobs))
}

pub async fn list_issues(State(state): State<AppState>) -> ServiceResult<Vec<SensitiveDataIssue>> {
    let rows = sqlx::query_as::<_, SensitiveDataIssueRow>(
        "SELECT id, job_id, kind, severity, status, matched_value, redacted_value, match_count, markings, remediation_actions, created_at, updated_at
         FROM sds_issues
         ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let mut issues = Vec::new();
    for row in rows {
        issues.push(sds::issue_from_row(row).map_err(internal_error)?);
    }
    Ok(Json(issues))
}

pub async fn mark_issue(
    State(state): State<AppState>,
    Path(issue_id): Path<Uuid>,
    Json(request): Json<MarkSensitiveIssueRequest>,
) -> ServiceResult<SensitiveDataIssue> {
    let row = sqlx::query_as::<_, SensitiveDataIssueRow>(
        "SELECT id, job_id, kind, severity, status, matched_value, redacted_value, match_count, markings, remediation_actions, created_at, updated_at
         FROM sds_issues WHERE id = $1",
    )
    .bind(issue_id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let Some(row) = row else {
        return Err((
            StatusCode::NOT_FOUND,
            Json(super::ErrorResponse {
                error: "issue not found".to_string(),
            }),
        ));
    };
    let issue = sds::issue_from_row(row).map_err(internal_error)?;
    let (markings, remediations, status) =
        sds::apply_markings(&issue, &request).map_err(internal_error)?;

    let row = sqlx::query_as::<_, SensitiveDataIssueRow>(
        "UPDATE sds_issues
         SET status = $2, markings = $3::jsonb, remediation_actions = $4::jsonb, updated_at = NOW()
         WHERE id = $1
         RETURNING id, job_id, kind, severity, status, matched_value, redacted_value, match_count, markings, remediation_actions, created_at, updated_at",
    )
    .bind(issue_id)
    .bind(status)
    .bind(markings)
    .bind(remediations)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(sds::issue_from_row(row).map_err(internal_error)?))
}

pub async fn list_remediation_rules(
    State(state): State<AppState>,
) -> ServiceResult<Vec<RemediationRule>> {
    let rows = sqlx::query_as::<_, (
        Uuid,
        String,
        String,
        serde_json::Value,
        serde_json::Value,
        Option<Uuid>,
        chrono::DateTime<chrono::Utc>,
        chrono::DateTime<chrono::Utc>,
    )>(
        "SELECT id, name, scope, match_conditions, remediation_actions, updated_by, created_at, updated_at
         FROM sds_remediation_rules
         ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let mut rules = Vec::new();
    for row in rows {
        rules.push(sds::rule_from_row(row).map_err(internal_error)?);
    }
    Ok(Json(rules))
}

pub async fn create_remediation_rule(
    State(state): State<AppState>,
    Json(request): Json<CreateRemediationRuleRequest>,
) -> ServiceResult<RemediationRule> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    if request.scope.trim().is_empty() {
        return Err(bad_request("scope is required"));
    }
    let (match_conditions, remediation_actions) =
        sds::rule_payload(&request).map_err(internal_error)?;

    let row = sqlx::query_as::<_, (
        Uuid,
        String,
        String,
        serde_json::Value,
        serde_json::Value,
        Option<Uuid>,
        chrono::DateTime<chrono::Utc>,
        chrono::DateTime<chrono::Utc>,
    )>(
        "INSERT INTO sds_remediation_rules (id, name, scope, match_conditions, remediation_actions, updated_by)
         VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6)
         RETURNING id, name, scope, match_conditions, remediation_actions, updated_by, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&request.name)
    .bind(&request.scope)
    .bind(match_conditions)
    .bind(remediation_actions)
    .bind(request.updated_by)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(sds::rule_from_row(row).map_err(internal_error)?))
}
