use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use auth_middleware::layer::AuthUser;
use serde_json::json;

use crate::{
    AppState,
    domain::workflow,
    models::workflow_execution::TriggerEventRequest,
};

pub async fn trigger_event(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(event_name): Path<String>,
    Json(body): Json<TriggerEventRequest>,
) -> impl IntoResponse {
    match workflow::trigger_event_workflows(&state, &event_name, claims.sub, body.context).await {
        Ok(runs) => Json(json!({ "data": runs, "event_name": event_name })).into_response(),
        Err(error) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": error })),
        )
            .into_response(),
    }
}

pub async fn run_due_cron_workflows(
    _user: AuthUser,
    State(state): State<AppState>,
) -> impl IntoResponse {
    match workflow::run_due_cron_workflows(&state).await {
        Ok(triggered_runs) => Json(json!({ "triggered_runs": triggered_runs })).into_response(),
        Err(error) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": error })),
        )
            .into_response(),
    }
}