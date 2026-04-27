use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde_json::{Map, Value};
use uuid::Uuid;

use crate::{
    AppState,
    models::{
        connection::Connection,
        sync_job::{SyncJob, SyncRequest},
        sync_status::SyncStatus,
    },
};

#[derive(Debug, Deserialize)]
pub struct InternalQueueSyncJobRequest {
    pub connection_id: Uuid,
    pub table_name: String,
    pub target_dataset_id: Option<Uuid>,
    pub schedule_at: Option<DateTime<Utc>>,
    pub max_attempts: Option<i32>,
    #[serde(default)]
    pub sync_metadata: Value,
}

/// POST /api/v1/connections/:id/sync
pub async fn sync_connection(
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
    Json(body): Json<SyncRequest>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, connection_id).await {
        Ok(Some(connection)) => connection,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("sync lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let scheduled_at = body.schedule_at.unwrap_or_else(chrono::Utc::now);
    let max_attempts = body.max_attempts.unwrap_or(3).clamp(1, 10);
    let job = match enqueue_sync_job(
        &state,
        &connection,
        body.target_dataset_id,
        &body.table_name,
        scheduled_at,
        max_attempts,
        Value::Null,
    )
    .await
    {
        Ok(job) => job,
        Err(error) => {
            tracing::error!("sync job insert failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    trigger_scheduler(&state, "sync scheduler trigger failed");

    (
        StatusCode::ACCEPTED,
        Json(serde_json::json!({
            "job_id": job.id,
            "status": job.status,
            "connection_id": job.connection_id,
            "scheduled_at": job.scheduled_at,
            "max_attempts": job.max_attempts,
        })),
    )
        .into_response()
}

pub async fn queue_internal_sync_job(
    State(state): State<AppState>,
    Json(body): Json<InternalQueueSyncJobRequest>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, body.connection_id).await {
        Ok(Some(connection)) => connection,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("internal sync lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let scheduled_at = body.schedule_at.unwrap_or_else(Utc::now);
    let max_attempts = body.max_attempts.unwrap_or(3).clamp(1, 10);
    let job = match enqueue_sync_job(
        &state,
        &connection,
        body.target_dataset_id,
        &body.table_name,
        scheduled_at,
        max_attempts,
        body.sync_metadata,
    )
    .await
    {
        Ok(job) => job,
        Err(error) => {
            tracing::error!("internal sync job insert failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    trigger_scheduler(&state, "internal sync scheduler trigger failed");

    (StatusCode::CREATED, Json(job)).into_response()
}

/// GET /api/v1/connections/:id/sync-jobs
pub async fn list_sync_jobs(
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
) -> impl IntoResponse {
    let jobs = sqlx::query_as::<_, SyncJob>(
        "SELECT * FROM sync_jobs WHERE connection_id = $1 ORDER BY created_at DESC LIMIT 50",
    )
    .bind(connection_id)
    .fetch_all(&state.db)
    .await;

    match jobs {
        Ok(j) => Json(j).into_response(),
        Err(e) => {
            tracing::error!("list sync jobs failed: {e}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn load_connection(
    state: &AppState,
    connection_id: Uuid,
) -> Result<Option<Connection>, String> {
    sqlx::query_as::<_, Connection>("SELECT * FROM connections WHERE id = $1")
        .bind(connection_id)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| error.to_string())
}

async fn enqueue_sync_job(
    state: &AppState,
    connection: &Connection,
    target_dataset_id: Option<Uuid>,
    table_name: &str,
    scheduled_at: DateTime<Utc>,
    max_attempts: i32,
    extra_sync_metadata: Value,
) -> Result<SyncJob, String> {
    let metadata = build_sync_metadata(connection, table_name, extra_sync_metadata);
    let job = sqlx::query_as::<_, SyncJob>(
        r#"INSERT INTO sync_jobs (
               id, connection_id, target_dataset_id, table_name, status, scheduled_at, max_attempts, sync_metadata
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
           RETURNING *"#,
    )
    .bind(Uuid::now_v7())
    .bind(connection.id)
    .bind(target_dataset_id)
    .bind(table_name)
    .bind(SyncStatus::Pending.as_str())
    .bind(scheduled_at)
    .bind(max_attempts)
    .bind(metadata)
    .fetch_one(&state.db)
    .await
    .map_err(|error| error.to_string())?;

    tracing::info!(
        connection_id = %connection.id,
        job_id = %job.id,
        connector_type = %connection.connector_type,
        target_dataset_id = ?target_dataset_id,
        "sync job queued"
    );

    Ok(job)
}

fn build_sync_metadata(connection: &Connection, table_name: &str, extra: Value) -> Value {
    let mut metadata = Map::new();
    metadata.insert(
        "selector".to_string(),
        Value::String(table_name.to_string()),
    );
    metadata.insert(
        "connector_type".to_string(),
        Value::String(connection.connector_type.clone()),
    );

    match extra {
        Value::Object(extra_object) => {
            for (key, value) in extra_object {
                metadata.insert(key, value);
            }
        }
        Value::Null => {}
        other => {
            metadata.insert("extra".to_string(), other);
        }
    }

    Value::Object(metadata)
}

fn trigger_scheduler(state: &AppState, warning: &'static str) {
    let scheduler_state = state.clone();
    tokio::spawn(async move {
        if let Err(error) = crate::domain::scheduler::tick(&scheduler_state).await {
            tracing::warn!("{warning}: {error}");
        }
    });
}
