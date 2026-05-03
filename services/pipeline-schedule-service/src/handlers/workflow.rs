use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;

use crate::{AppState, domain::workflow, models::workflow_execution::TriggerEventRequest};

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

