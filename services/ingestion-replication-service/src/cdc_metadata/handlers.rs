use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::cdc_metadata::{
    AppState,
    models::{
        CdcStream, IncrementalCheckpoint, RecordCheckpointRequest, RegisterCdcStreamRequest,
        ResolutionState, UpdateResolutionRequest,
    },
};

fn db_error(label: &str, error: sqlx::Error) -> axum::response::Response {
    tracing::error!("cdc-metadata-service {label} failed: {error}");
    StatusCode::INTERNAL_SERVER_ERROR.into_response()
}

pub async fn list_streams(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, CdcStream>(
        "SELECT id, slug, source_kind, source_ref, upstream_topic, primary_keys, watermark_column,
                incremental_mode, status, created_at, updated_at
         FROM cdc_streams
         ORDER BY slug",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_streams", error),
    }
}

pub async fn register_stream(
    State(state): State<AppState>,
    Json(body): Json<RegisterCdcStreamRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let primary_keys = match serde_json::to_value(&body.primary_keys) {
        Ok(value) => value,
        Err(error) => {
            tracing::error!("serialize primary keys failed: {error}");
            return StatusCode::BAD_REQUEST.into_response();
        }
    };

    let result = sqlx::query_as::<_, CdcStream>(
        "INSERT INTO cdc_streams (id, slug, source_kind, source_ref, upstream_topic, primary_keys,
                                  watermark_column, incremental_mode, status)
         VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, 'registered')
         ON CONFLICT (slug) DO UPDATE
         SET source_kind = EXCLUDED.source_kind,
             source_ref = EXCLUDED.source_ref,
             upstream_topic = EXCLUDED.upstream_topic,
             primary_keys = EXCLUDED.primary_keys,
             watermark_column = EXCLUDED.watermark_column,
             incremental_mode = EXCLUDED.incremental_mode,
             updated_at = NOW()
         RETURNING id, slug, source_kind, source_ref, upstream_topic, primary_keys,
                   watermark_column, incremental_mode, status, created_at, updated_at",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.source_kind)
    .bind(&body.source_ref)
    .bind(body.upstream_topic.as_deref())
    .bind(primary_keys)
    .bind(body.watermark_column.as_deref())
    .bind(&body.incremental_mode)
    .fetch_one(&state.db)
    .await;

    match result {
        Ok(stream) => {
            // Seed empty checkpoint and resolution rows for downstream pollers.
            let _ = sqlx::query(
                "INSERT INTO cdc_incremental_checkpoints (stream_id) VALUES ($1)
                 ON CONFLICT (stream_id) DO NOTHING",
            )
            .bind(stream.id)
            .execute(&state.db)
            .await;

            let _ = sqlx::query(
                "INSERT INTO cdc_resolution_state (stream_id, status) VALUES ($1, 'lagging')
                 ON CONFLICT (stream_id) DO NOTHING",
            )
            .bind(stream.id)
            .execute(&state.db)
            .await;

            (StatusCode::CREATED, Json(stream)).into_response()
        }
        Err(error) => db_error("register_stream", error),
    }
}

pub async fn get_stream(State(state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    match sqlx::query_as::<_, CdcStream>(
        "SELECT id, slug, source_kind, source_ref, upstream_topic, primary_keys, watermark_column,
                incremental_mode, status, created_at, updated_at
         FROM cdc_streams WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(stream)) => Json(stream).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_stream", error),
    }
}

pub async fn record_checkpoint(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<RecordCheckpointRequest>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, IncrementalCheckpoint>(
        "INSERT INTO cdc_incremental_checkpoints (stream_id, last_offset, last_lsn, last_event_at,
                                                  records_observed, records_applied)
         VALUES ($1, $2, $3, $4, $5, $6)
         ON CONFLICT (stream_id) DO UPDATE
         SET last_offset = COALESCE(EXCLUDED.last_offset, cdc_incremental_checkpoints.last_offset),
             last_lsn = COALESCE(EXCLUDED.last_lsn, cdc_incremental_checkpoints.last_lsn),
             last_event_at = COALESCE(EXCLUDED.last_event_at, cdc_incremental_checkpoints.last_event_at),
             records_observed = cdc_incremental_checkpoints.records_observed + EXCLUDED.records_observed,
             records_applied = cdc_incremental_checkpoints.records_applied + EXCLUDED.records_applied,
             updated_at = NOW()
         RETURNING stream_id, last_offset, last_lsn, last_event_at, records_observed, records_applied, updated_at",
    )
    .bind(id)
    .bind(body.last_offset.as_deref())
    .bind(body.last_lsn.as_deref())
    .bind(body.last_event_at)
    .bind(body.records_observed)
    .bind(body.records_applied)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => Json(row).into_response(),
        Err(error) => db_error("record_checkpoint", error),
    }
}

pub async fn get_checkpoint(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, IncrementalCheckpoint>(
        "SELECT stream_id, last_offset, last_lsn, last_event_at, records_observed, records_applied, updated_at
         FROM cdc_incremental_checkpoints WHERE stream_id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_checkpoint", error),
    }
}

pub async fn get_resolution(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ResolutionState>(
        "SELECT stream_id, status, watermark, conflict_count, pending_resolutions, notes, updated_at
         FROM cdc_resolution_state WHERE stream_id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_resolution", error),
    }
}

pub async fn update_resolution(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateResolutionRequest>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ResolutionState>(
        "INSERT INTO cdc_resolution_state (stream_id, status, watermark, conflict_count, pending_resolutions, notes)
         VALUES ($1, $2, $3, $4, $5, $6)
         ON CONFLICT (stream_id) DO UPDATE
         SET status = EXCLUDED.status,
             watermark = COALESCE(EXCLUDED.watermark, cdc_resolution_state.watermark),
             conflict_count = EXCLUDED.conflict_count,
             pending_resolutions = EXCLUDED.pending_resolutions,
             notes = EXCLUDED.notes,
             updated_at = NOW()
         RETURNING stream_id, status, watermark, conflict_count, pending_resolutions, notes, updated_at",
    )
    .bind(id)
    .bind(&body.status)
    .bind(body.watermark)
    .bind(body.conflict_count)
    .bind(body.pending_resolutions)
    .bind(body.notes.as_deref())
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => Json(row).into_response(),
        Err(error) => db_error("update_resolution", error),
    }
}
