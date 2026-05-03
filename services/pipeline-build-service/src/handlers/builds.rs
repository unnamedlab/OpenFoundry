//! Builds queue surface (Foundry: "Builds" application).
//!
//! Provides cross-pipeline visibility of every `pipeline_run` row plus
//! an abort path for runs still in `running`. The single-pipeline
//! list/get endpoints live in `runs.rs` and `execute.rs` (shimmed from
//! `pipeline-authoring-service`) — this module only adds the global
//! surface that Pipeline Builder's UI cannot synthesize from per-pipeline
//! calls.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Deserialize;
use serde_json::json;
use uuid::Uuid;

use crate::AppState;
use crate::models::run::PipelineRun;

/// Filters for the global queue (mirrors Foundry's "Builds" app filter bar).
///
/// `status` accepts the same vocabulary used by the executor when it stamps
/// `pipeline_runs.status`: `running`, `completed`, `failed`, `aborted`.
/// `trigger_type` mirrors `pipeline_runs.trigger_type` (`manual`,
/// `scheduled`, `event`, `retry`).
#[derive(Debug, Deserialize)]
pub struct BuildQueueQuery {
    pub status: Option<String>,
    pub trigger_type: Option<String>,
    pub pipeline_id: Option<Uuid>,
    pub page: Option<i64>,
    pub per_page: Option<i64>,
}

pub async fn list_builds(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<BuildQueueQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(50).clamp(1, 200);
    let offset = (page - 1) * per_page;

    let runs = sqlx::query_as::<_, PipelineRun>(
        r#"SELECT * FROM pipeline_runs
           WHERE ($1::text IS NULL OR status = $1)
             AND ($2::text IS NULL OR trigger_type = $2)
             AND ($3::uuid IS NULL OR pipeline_id = $3)
           ORDER BY started_at DESC
           LIMIT $4 OFFSET $5"#,
    )
    .bind(params.status.as_deref())
    .bind(params.trigger_type.as_deref())
    .bind(params.pipeline_id)
    .bind(per_page)
    .bind(offset)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    Json(json!({ "data": runs, "page": page, "per_page": per_page })).into_response()
}

/// Abort a running build (Foundry "Cancel build" button). Marks the row
/// `aborted` only when it is still `running`; other states are returned
/// unchanged with `409 Conflict`.
pub async fn abort_build(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(run_id): Path<Uuid>,
) -> impl IntoResponse {
    let updated = sqlx::query_as::<_, PipelineRun>(
        r#"UPDATE pipeline_runs
           SET status = 'aborted',
               error_message = COALESCE(error_message, 'aborted by user'),
               finished_at = NOW()
           WHERE id = $1 AND status = 'running'
           RETURNING *"#,
    )
    .bind(run_id)
    .fetch_optional(&state.db)
    .await;

    match updated {
        Ok(Some(run)) => (StatusCode::OK, Json(json!(run))).into_response(),
        Ok(None) => {
            // Either the run does not exist, or it is no longer running.
            let exists = sqlx::query_scalar::<_, bool>(
                "SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE id = $1)",
            )
            .bind(run_id)
            .fetch_one(&state.db)
            .await
            .unwrap_or(false);
            if exists {
                StatusCode::CONFLICT.into_response()
            } else {
                StatusCode::NOT_FOUND.into_response()
            }
        }
        Err(error) => (StatusCode::INTERNAL_SERVER_ERROR, error.to_string()).into_response(),
    }
}

/// Aggregate counters by status for the queue dashboard.
pub async fn queue_summary(_user: AuthUser, State(state): State<AppState>) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, (String, i64)>(
        r#"SELECT status, COUNT(*)::bigint
           FROM pipeline_runs
           WHERE started_at > NOW() - INTERVAL '24 hours'
           GROUP BY status"#,
    )
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    let mut summary = serde_json::Map::new();
    for (status, count) in rows {
        summary.insert(status, json!(count));
    }
    Json(json!({ "last_24h": summary })).into_response()
}
