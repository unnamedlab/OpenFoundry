//! Foundry-parity stream-view handlers.
//!
//! Hosts the `Reset stream` workflow and the read-side of the
//! `streaming_stream_views` table:
//!   * `POST /v1/streams/{id}/reset` — rotate viewRid + clear records.
//!   * `GET  /v1/streams/{id}/views` — full history (UI History tab).
//!   * `GET  /v1/streams/{id}/current-view` — current active view.
//!
//! The push proxy lives in [`super::push_proxy`] and consumes the
//! lookup helper [`load_active_view_by_view_rid`] exposed here.

use auth_middleware::claims::Claims;
use axum::{Extension, Json, extract::Path};
use chrono::Utc;
use serde_json::Value;
use sqlx::{PgPool, Postgres, Transaction, types::Json as SqlJson};
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{
        ServiceResult, conflict, db_error, forbidden, internal_error, not_found, unprocessable,
    },
    models::{
        stream::{StreamDefinition, StreamRow},
        stream_view::{
            ListViewsResponse, ResetStreamRequest, ResetStreamResponse, StreamKind, StreamView,
            stream_rid_for, view_rid_for,
        },
    },
    outbox as streaming_outbox,
};

/// Stable error codes surfaced by the reset endpoint.
pub const ERR_RESET_REQUIRES_INGEST: &str = "STREAM_RESET_ONLY_INGEST_KIND";
pub const ERR_RESET_DOWNSTREAM_ACTIVE: &str = "STREAM_RESET_DOWNSTREAM_PIPELINES_ACTIVE";

/// Permission key required to mutate streams.
const PERM_STREAM_WRITE: &str = "streaming:write";

fn can_write_streams(claims: &Claims) -> bool {
    claims.has_any_role(&["admin", "streaming_admin", "data_engineer"])
        || claims.has_permission_key(PERM_STREAM_WRITE)
}

async fn load_stream_row(db: &PgPool, id: Uuid) -> Result<StreamRow, sqlx::Error> {
    sqlx::query_as::<_, StreamRow>(
        "SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, stream_type, compression, ingest_consistency, pipeline_consistency, checkpoint_interval_ms, kind, created_at, updated_at
         FROM streaming_streams
         WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
    .await
}

async fn load_active_view_tx(
    tx: &mut Transaction<'_, Postgres>,
    stream_rid: &str,
) -> Result<Option<StreamView>, sqlx::Error> {
    sqlx::query_as::<_, StreamView>(
        "SELECT id, stream_rid, view_rid, schema_json, config_json, generation, active,
                created_by, created_at, retired_at
         FROM streaming_stream_views
         WHERE stream_rid = $1 AND active = true
         ORDER BY generation DESC
         LIMIT 1",
    )
    .bind(stream_rid)
    .fetch_optional(&mut **tx)
    .await
}

/// Look up an active view by its `view_rid`. Used by the push proxy to
/// answer 404 `PUSH_VIEW_RETIRED` when an old POST URL hits the
/// gateway.
pub async fn load_active_view_by_view_rid(
    db: &PgPool,
    view_rid: &str,
) -> Result<Option<StreamView>, sqlx::Error> {
    sqlx::query_as::<_, StreamView>(
        "SELECT id, stream_rid, view_rid, schema_json, config_json, generation, active,
                created_by, created_at, retired_at
         FROM streaming_stream_views
         WHERE view_rid = $1",
    )
    .bind(view_rid)
    .fetch_optional(db)
    .await
}

async fn load_active_view_by_stream_rid(
    db: &PgPool,
    stream_rid: &str,
) -> Result<Option<StreamView>, sqlx::Error> {
    sqlx::query_as::<_, StreamView>(
        "SELECT id, stream_rid, view_rid, schema_json, config_json, generation, active,
                created_by, created_at, retired_at
         FROM streaming_stream_views
         WHERE stream_rid = $1 AND active = true
         ORDER BY generation DESC
         LIMIT 1",
    )
    .bind(stream_rid)
    .fetch_optional(db)
    .await
}

fn build_push_url(base_url: &str, view_rid: &str) -> String {
    let trimmed = base_url.trim_end_matches('/');
    format!("{trimmed}/streams-push/{view_rid}/records")
}

/// Returns true when at least one running topology references this
/// stream. The check is conservative — any non-`stopped`/`failed`
/// status counts.
async fn downstream_pipelines_active(db: &PgPool, stream_id: Uuid) -> Result<bool, sqlx::Error> {
    // `source_stream_ids` is JSONB. We scan the array for the stream id
    // and skip topologies whose `status` is in a terminal state.
    let count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*)
           FROM streaming_topologies
          WHERE status NOT IN ('stopped', 'failed', 'archived')
            AND source_stream_ids @> jsonb_build_array($1::text)::jsonb",
    )
    .bind(stream_id.to_string())
    .fetch_one(db)
    .await?;
    Ok(count > 0)
}

/// `POST /api/v1/streaming/streams/{id}/reset`
///
/// Rotates the stream's `viewRid` so push consumers must re-fetch the
/// POST URL. Atomic: marks the previous view retired, inserts the
/// fresh view (generation+1), updates `streaming_streams` with the new
/// schema/config when supplied, and emits `stream.reset.v1` over the
/// outbox so downstream services can react.
pub async fn reset_stream(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
    Json(payload): Json<ResetStreamRequest>,
) -> ServiceResult<ResetStreamResponse> {
    if !can_write_streams(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let stream_row = match load_stream_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let stream: StreamDefinition = stream_row.into();

    if !matches!(stream.kind, StreamKind::Ingest) {
        return Err(unprocessable(
            ERR_RESET_REQUIRES_INGEST,
            "resets are only available for ingest streams",
        ));
    }

    if !payload.force {
        let active = downstream_pipelines_active(&state.db, stream.id)
            .await
            .map_err(|cause| db_error(&cause))?;
        if active {
            return Err(conflict(
                ERR_RESET_DOWNSTREAM_ACTIVE,
                "downstream pipelines are still active; pass force=true after acknowledging the replay requirement",
            ));
        }
    }

    let stream_rid = stream_rid_for(stream.id);
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;

    // 1. Retire the current active view (if any).
    let previous = load_active_view_tx(&mut tx, &stream_rid)
        .await
        .map_err(|cause| db_error(&cause))?;
    let previous_generation = previous.as_ref().map(|v| v.generation).unwrap_or(0);
    let previous_view_rid = previous
        .as_ref()
        .map(|v| v.view_rid.clone())
        .unwrap_or_else(|| view_rid_for(stream.id));
    let now = Utc::now();
    sqlx::query(
        "UPDATE streaming_stream_views
            SET active = false,
                retired_at = $2
          WHERE stream_rid = $1 AND active = true",
    )
    .bind(&stream_rid)
    .bind(now)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    // 2. Resolve the schema + config snapshot to install on the fresh
    //    view. Falls back to the previous view's snapshot when the
    //    body left the field empty (clear records, keep shape).
    let schema_for_view: Value = payload.new_schema.clone().unwrap_or_else(|| {
        previous
            .as_ref()
            .and_then(|v| v.schema_json.as_ref().map(|j| j.0.clone()))
            .unwrap_or_else(|| serde_json::to_value(&stream.schema).unwrap_or(Value::Null))
    });
    let config_for_view: Value = payload.new_config.clone().unwrap_or_else(|| {
        previous
            .as_ref()
            .and_then(|v| v.config_json.as_ref().map(|j| j.0.clone()))
            .unwrap_or_else(|| serde_json::to_value(stream.config_view()).unwrap_or(Value::Null))
    });
    let schema_changed = previous
        .as_ref()
        .and_then(|v| v.schema_json.as_ref().map(|j| &j.0))
        .map(|prev| prev != &schema_for_view)
        .unwrap_or(true);
    let config_changed = previous
        .as_ref()
        .and_then(|v| v.config_json.as_ref().map(|j| &j.0))
        .map(|prev| prev != &config_for_view)
        .unwrap_or(true);

    // 3. Mint the fresh view RID. UUID v7 keeps view RIDs roughly
    //    sortable so the History tab can render them in creation
    //    order without an extra column.
    let new_uuid = Uuid::now_v7();
    let new_view_rid = view_rid_for(new_uuid);
    let new_generation = previous_generation + 1;
    sqlx::query(
        "INSERT INTO streaming_stream_views (
             id, stream_rid, view_rid, schema_json, config_json, generation, active, created_by
         ) VALUES ($1, $2, $3, $4, $5, $6, true, $7)",
    )
    .bind(new_uuid)
    .bind(&stream_rid)
    .bind(&new_view_rid)
    .bind(SqlJson(schema_for_view.clone()))
    .bind(SqlJson(config_for_view.clone()))
    .bind(new_generation)
    .bind(claims.sub.to_string())
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    // 4. Reload the freshly inserted row so we own a `StreamView` we
    //    can hand back to the caller verbatim.
    let new_view: StreamView = sqlx::query_as::<_, StreamView>(
        "SELECT id, stream_rid, view_rid, schema_json, config_json, generation, active,
                created_by, created_at, retired_at
           FROM streaming_stream_views
          WHERE view_rid = $1",
    )
    .bind(&new_view_rid)
    .fetch_one(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    // 5. Apply the optional new schema to `streaming_streams.schema`
    //    so subsequent push validations honour it.
    if let Some(schema_json) = payload.new_schema.as_ref() {
        sqlx::query(
            "UPDATE streaming_streams
                SET schema = $2,
                    updated_at = now()
              WHERE id = $1",
        )
        .bind(stream.id)
        .bind(SqlJson(schema_json))
        .execute(&mut *tx)
        .await
        .map_err(|cause| db_error(&cause))?;
    }

    // 6. Emit `stream.reset.v1` to the transactional outbox.
    let claims_sub = claims.sub.to_string();
    let event = streaming_outbox::stream_reset(
        &stream,
        &previous_view_rid,
        &new_view,
        &claims_sub,
        payload.force,
    );
    streaming_outbox::emit(&mut tx, &event)
        .await
        .map_err(|cause| {
            tracing::error!(stream_id = %stream.id, error = %cause, "failed to enqueue stream.reset outbox event");
            internal_error("failed to enqueue outbox event")
        })?;

    tx.commit().await.map_err(|cause| db_error(&cause))?;

    // 7. Best-effort hot buffer rotation.
    //    For Kafka we materialise a fresh per-generation topic so the
    //    previous one stays available under retention until TTL.
    //    Failures are logged — the metadata is already persisted and
    //    operators can retry.
    if let Err(err) = state
        .hot_buffer
        .ensure_topic(stream.id, stream.partitions)
        .await
    {
        tracing::warn!(
            stream_id = %stream.id,
            error = %err,
            "hot buffer ensure_topic failed during reset_stream"
        );
    }

    // 8. Audit emission — `audit-trail::middleware` collects events
    //    on the `audit` tracing target.
    tracing::info!(
        target: "audit",
        event = "stream.reset",
        actor.sub = %claims.sub,
        actor.email = %claims.email,
        resource.type = "streaming_stream",
        resource.id = %stream.id,
        stream_rid = %new_view.stream_rid,
        old_view_rid = %previous_view_rid,
        new_view_rid = %new_view.view_rid,
        generation = new_view.generation,
        schema_changed = schema_changed,
        config_changed = config_changed,
        forced = payload.force,
        "streaming audit event"
    );

    let push_url = build_push_url(&state.public_base_url, &new_view.view_rid);
    Ok(Json(ResetStreamResponse {
        stream_rid: stream_rid.clone(),
        old_view_rid: previous_view_rid,
        new_view_rid: new_view.view_rid.clone(),
        generation: new_view.generation,
        view: new_view,
        push_url,
        forced: payload.force,
    }))
}

/// `GET /api/v1/streaming/streams/{id}/views`
pub async fn list_stream_views(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(id): Path<Uuid>,
) -> ServiceResult<ListViewsResponse> {
    // Confirm the stream exists so callers get 404 instead of an
    // empty list when they typo the id.
    if let Err(sqlx::Error::RowNotFound) = load_stream_row(&state.db, id).await {
        return Err(not_found("stream not found"));
    }

    let stream_rid = stream_rid_for(id);
    let rows = sqlx::query_as::<_, StreamView>(
        "SELECT id, stream_rid, view_rid, schema_json, config_json, generation, active,
                created_by, created_at, retired_at
           FROM streaming_stream_views
          WHERE stream_rid = $1
          ORDER BY generation DESC",
    )
    .bind(&stream_rid)
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(ListViewsResponse { data: rows }))
}

/// Helper to answer the push proxy's `GET /streams-push/{stream_rid}/url`
/// (registered separately under the unauthenticated outer router).
pub async fn current_view_for_stream_rid(
    db: &PgPool,
    stream_rid: &str,
) -> Result<Option<StreamView>, sqlx::Error> {
    load_active_view_by_stream_rid(db, stream_rid).await
}

/// Same helper but resolves by stream UUID — useful for the
/// authenticated `GET /v1/streams/{id}/current-view` endpoint.
pub async fn get_current_view(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(id): Path<Uuid>,
) -> ServiceResult<StreamView> {
    if let Err(sqlx::Error::RowNotFound) = load_stream_row(&state.db, id).await {
        return Err(not_found("stream not found"));
    }
    let stream_rid = stream_rid_for(id);
    match load_active_view_by_stream_rid(&state.db, &stream_rid)
        .await
        .map_err(|cause| db_error(&cause))?
    {
        Some(view) => Ok(Json(view)),
        None => Err(not_found("stream has no active view")),
    }
}

/// Internal helper used by tests: surface the `push_url` builder so
/// integration tests can assert the format Foundry consumers expect.
pub fn render_push_url(base_url: &str, view_rid: &str) -> String {
    build_push_url(base_url, view_rid)
}

/// Helper exposed for the push proxy's `GET /url` endpoint.
pub fn render_push_url_response(
    stream_rid: String,
    view: &StreamView,
    base_url: &str,
) -> crate::models::stream_view::PushUrlResponse {
    crate::models::stream_view::PushUrlResponse {
        stream_rid,
        view_rid: view.view_rid.clone(),
        generation: view.generation,
        push_url: build_push_url(base_url, &view.view_rid),
        note: "URL will rotate on next reset; consumers must re-fetch after stream.reset.v1"
            .to_string(),
    }
}
