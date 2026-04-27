use axum::{
    Json,
    extract::{Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use auth_middleware::layer::AuthUser;
use serde_json::json;

use crate::{
    AppState,
    domain::schedule,
    models::schedule::{BackfillScheduleRequest, ListDueRunsQuery, PreviewScheduleWindowsRequest},
};

fn error_status(error: &str) -> StatusCode {
    if error.contains("not found") {
        StatusCode::NOT_FOUND
    } else {
        StatusCode::BAD_REQUEST
    }
}

pub async fn list_due_runs(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<ListDueRunsQuery>,
) -> impl IntoResponse {
    let limit = schedule::clamp_limit(params.limit);

    match schedule::list_due_runs(&state, params.kind, limit).await {
        Ok(data) => Json(json!({ "data": data, "total": data.len() })).into_response(),
        Err(error) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": error })),
        )
            .into_response(),
    }
}

pub async fn preview_windows(
    _user: AuthUser,
    State(state): State<AppState>,
    Json(body): Json<PreviewScheduleWindowsRequest>,
) -> impl IntoResponse {
    let PreviewScheduleWindowsRequest {
        target_kind,
        target_id,
        start_at,
        end_at,
        limit,
    } = body;
    let limit = schedule::clamp_limit(limit);

    match schedule::preview_windows(&state, target_kind, target_id, start_at, end_at, limit).await {
        Ok(data) => Json(json!({
            "target_kind": target_kind,
            "target_id": target_id,
            "data": data,
        }))
        .into_response(),
        Err(error) => (
            error_status(&error),
            Json(json!({ "error": error })),
        )
            .into_response(),
    }
}

pub async fn backfill_runs(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<BackfillScheduleRequest>,
) -> impl IntoResponse {
    let BackfillScheduleRequest {
        target_kind,
        target_id,
        start_at,
        end_at,
        limit,
        dry_run,
        context,
        skip_unchanged,
    } = body;
    let limit = schedule::clamp_limit(limit);

    match schedule::backfill_runs(
        &state,
        target_kind,
        target_id,
        start_at,
        end_at,
        limit,
        dry_run,
        context,
        skip_unchanged,
        Some(claims.sub),
    )
    .await
    {
        Ok(data) => Json(json!({
            "target_kind": target_kind,
            "target_id": target_id,
            "dry_run": dry_run,
            "data": data,
        }))
        .into_response(),
        Err(error) => (
            error_status(&error),
            Json(json!({ "error": error })),
        )
            .into_response(),
    }
}