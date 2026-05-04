//! `GET /v1/schedules/{rid}/troubleshoot` — Foundry-parity
//! Troubleshooting endpoint.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use sqlx::Row;
use std::collections::HashSet;

use crate::AppState;
use crate::domain::run_store::{self, ListRunsFilter};
use crate::domain::schedule_store;
use crate::domain::troubleshoot::troubleshoot_schedule;

pub async fn get_report(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(schedule_store::StoreError::NotFound(_)) => {
            return StatusCode::NOT_FOUND.into_response();
        }
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({"error": e.to_string()})),
            )
                .into_response();
        }
    };

    let observed_paths: HashSet<String> = match sqlx::query(
        "SELECT trigger_path FROM schedule_event_observations WHERE schedule_id = $1",
    )
    .bind(schedule.id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => rows
            .iter()
            .filter_map(|r| r.try_get::<String, _>("trigger_path").ok())
            .collect(),
        Err(_) => HashSet::new(),
    };

    let recent_runs = run_store::list_for_schedule(
        &state.db,
        schedule.id,
        ListRunsFilter {
            limit: 50,
            ..Default::default()
        },
    )
    .await
    .unwrap_or_default();

    let report = troubleshoot_schedule(&schedule, &observed_paths, &recent_runs);
    Json(report).into_response()
}
