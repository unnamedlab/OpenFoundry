use auth_middleware::claims::Claims;
use axum::{
    Extension, Json,
    extract::{Path, Query},
};
use chrono::{DateTime, Utc};
use event_bus_control::schema_registry::{self, SchemaType};
use serde::Deserialize;
use serde_json::Value;
use sqlx::{Postgres, Transaction, types::Json as SqlJson};
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{ErrorResponse, ServiceResult, bad_request, db_error, forbidden, not_found},
    models::{
        ListResponse,
        dead_letter::{
            ReplayDeadLetterRequest, ReplayDeadLetterResponse, StreamingDeadLetter,
            StreamingDeadLetterRow,
        },
        stream::{
            ConnectorBinding, CreateStreamRequest, PushStreamEventsRequest,
            PushStreamEventsResponse, StreamConfig, StreamConsistency, StreamDefinition, StreamRow,
            StreamSchema, StreamType, UpdateStreamConfigRequest, UpdateStreamRequest,
        },
        window::{CreateWindowRequest, UpdateWindowRequest, WindowDefinition, WindowRow},
    },
    outbox as streaming_outbox,
};

/// Allowed stream-type values surfaced in error messages.
const STREAM_TYPE_VALUES: &str =
    "STANDARD, HIGH_THROUGHPUT, COMPRESSED, HIGH_THROUGHPUT_COMPRESSED";

/// Foundry-documented partition cap. `1..=50` per the docs throughput
/// slider ("each partition increases throughput by ~5 MB/s").
const PARTITIONS_MIN: i32 = 1;
const PARTITIONS_MAX: i32 = 50;

/// Stable error code surfaced when callers attempt to set
/// `ingest_consistency = EXACTLY_ONCE`. Documented response on the
/// `/config` endpoint.
const ERR_INGEST_EXACTLY_ONCE: &str = "STREAM_INGEST_EXACTLY_ONCE_NOT_SUPPORTED";

/// Stable error code surfaced when callers try to shrink the partition
/// count of an existing stream — Kafka does not support shrinking.
const ERR_PARTITIONS_SHRINK: &str = "STREAM_PARTITIONS_SHRINK_NOT_SUPPORTED";

fn unprocessable(
    code: &str,
    message: impl Into<String>,
) -> (axum::http::StatusCode, Json<ErrorResponse>) {
    (
        axum::http::StatusCode::UNPROCESSABLE_ENTITY,
        Json(ErrorResponse {
            error: format!("{code}: {}", message.into()),
        }),
    )
}

fn conflict(
    code: &str,
    message: impl Into<String>,
) -> (axum::http::StatusCode, Json<ErrorResponse>) {
    (
        axum::http::StatusCode::CONFLICT,
        Json(ErrorResponse {
            error: format!("{code}: {}", message.into()),
        }),
    )
}

/// Permission key required to mutate streams (create/update/delete/push).
const PERM_STREAM_WRITE: &str = "streaming:write";

/// Returns true when the caller can mutate streams. Admins always pass.
fn can_write_streams(claims: &Claims) -> bool {
    claims.has_any_role(&["admin", "streaming_admin", "data_engineer"])
        || claims.has_permission_key(PERM_STREAM_WRITE)
}

/// Returns true when the caller is allowed to read a stream guarded by
/// `default_marking`. `None` marking means "no restriction".
fn caller_allowed_for_marking(claims: &Claims, marking: Option<&str>) -> bool {
    match marking {
        None => true,
        Some(name) if name.is_empty() => true,
        Some(name) => claims.allows_marking(name),
    }
}

/// Compute the canonical SHA-256 fingerprint of an Avro schema document.
pub(crate) fn compute_avro_fingerprint(schema: &Value) -> Result<String, String> {
    let text = serde_json::to_string(schema).map_err(|e| e.to_string())?;
    schema_registry::fingerprint(SchemaType::Avro, &text).map_err(|e| e.to_string())
}

/// Append a row to `streaming_stream_schema_history`.
pub(crate) async fn insert_schema_history_row(
    tx: &mut Transaction<'_, Postgres>,
    stream_id: Uuid,
    version: i32,
    schema: &Value,
    fingerprint: &str,
    compatibility: &str,
    created_by: &str,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        "INSERT INTO streaming_stream_schema_history (
             id, stream_id, version, schema_avro, fingerprint, compatibility, created_by
         ) VALUES ($1, $2, $3, $4, $5, $6, $7)
         ON CONFLICT (stream_id, version) DO NOTHING",
    )
    .bind(Uuid::now_v7())
    .bind(stream_id)
    .bind(version)
    .bind(SqlJson(schema))
    .bind(fingerprint)
    .bind(compatibility)
    .bind(created_by)
    .execute(&mut **tx)
    .await?;
    Ok(())
}

/// Best-effort audit emitter — produces a structured trace event under
/// the `audit` target so `audit-trail::middleware::AuditLayer`'s tracer
/// pipeline picks it up. Schema mirrors `audit-compliance-service`.
pub(crate) fn emit_audit_event(
    actor: &Claims,
    event: &str,
    resource_type: &str,
    resource_id: Uuid,
    extra: serde_json::Value,
) {
    tracing::info!(
        target: "audit",
        event = event,
        actor.sub = %actor.sub,
        actor.email = %actor.email,
        resource.type = resource_type,
        resource.id = %resource_id,
        extra = %extra,
        "streaming audit event"
    );
}

async fn load_stream_row(db: &sqlx::PgPool, id: Uuid) -> Result<StreamRow, sqlx::Error> {
    sqlx::query_as::<_, StreamRow>(
		"SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, stream_type, compression, ingest_consistency, pipeline_consistency, checkpoint_interval_ms, kind, created_at, updated_at
		 FROM streaming_streams
		 WHERE id = $1",
	)
	.bind(id)
	.fetch_one(db)
	.await
}

async fn load_stream_row_tx(
    tx: &mut Transaction<'_, Postgres>,
    id: Uuid,
) -> Result<StreamRow, sqlx::Error> {
    sqlx::query_as::<_, StreamRow>(
        "SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, stream_type, compression, ingest_consistency, pipeline_consistency, checkpoint_interval_ms, kind, created_at, updated_at
         FROM streaming_streams
         WHERE id = $1",
    )
    .bind(id)
    .fetch_one(&mut **tx)
    .await
}

async fn load_window_row(db: &sqlx::PgPool, id: Uuid) -> Result<WindowRow, sqlx::Error> {
    sqlx::query_as::<_, WindowRow>(
        "SELECT id, name, description, status, window_type, duration_seconds, slide_seconds,
		        session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields,
		        keyed, key_columns, state_ttl_seconds,
		        created_at, updated_at
		 FROM streaming_windows
		 WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
    .await
}

async fn load_window_row_tx(
    tx: &mut Transaction<'_, Postgres>,
    id: Uuid,
) -> Result<WindowRow, sqlx::Error> {
    sqlx::query_as::<_, WindowRow>(
        "SELECT id, name, description, status, window_type, duration_seconds, slide_seconds,
                session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields,
		        keyed, key_columns, state_ttl_seconds,
                created_at, updated_at
         FROM streaming_windows
         WHERE id = $1",
    )
    .bind(id)
    .fetch_one(&mut **tx)
    .await
}

async fn load_dead_letter_row(
    db: &sqlx::PgPool,
    id: Uuid,
) -> Result<StreamingDeadLetterRow, sqlx::Error> {
    sqlx::query_as::<_, StreamingDeadLetterRow>(
        "SELECT id, stream_id, payload, event_time, reason, validation_errors, status,
                replay_count, last_replayed_at, created_at, updated_at
         FROM streaming_dead_letters
         WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
    .await
}

fn field_present(payload: &serde_json::Map<String, Value>, name: &str) -> bool {
    payload.get(name).is_some_and(|value| !value.is_null())
}

fn field_type_matches(value: &Value, data_type: &str) -> bool {
    match data_type {
        "string" => value.is_string(),
        "integer" | "int64" => value.as_i64().is_some(),
        "float" | "float64" => value.as_f64().is_some(),
        "boolean" => value.is_boolean(),
        "timestamp" => value
            .as_str()
            .and_then(|text| chrono::DateTime::parse_from_rfc3339(text).ok())
            .is_some(),
        "array" | "list" => value.is_array(),
        "json" | "struct" | "object" => value.is_object(),
        _ => true,
    }
}

fn validate_event_against_schema(
    schema: &StreamSchema,
    payload: &Value,
    event_time: chrono::DateTime<Utc>,
) -> Result<(), Vec<String>> {
    let Some(object) = payload.as_object() else {
        return Err(vec![
            "stream event payload must be a JSON object".to_string(),
        ]);
    };

    let mut errors = Vec::new();
    for field in &schema.fields {
        let is_watermark = schema.watermark_field.as_deref() == Some(field.name.as_str())
            || field.semantic_role == "event_time";
        if !field.nullable && !field_present(object, &field.name) && !is_watermark {
            errors.push(format!("missing required field '{}'", field.name));
            continue;
        }
        if let Some(value) = object.get(&field.name) {
            if value.is_null() && !field.nullable && !is_watermark {
                errors.push(format!("field '{}' cannot be null", field.name));
                continue;
            }
            if !value.is_null() && !field_type_matches(value, &field.data_type) {
                errors.push(format!(
                    "field '{}' does not match declared type '{}'",
                    field.name, field.data_type
                ));
            }
        } else if is_watermark && field.data_type == "timestamp" {
            let _ = event_time;
        }
    }

    if let Some(watermark_field) = schema.watermark_field.as_deref() {
        if let Some(value) = object.get(watermark_field) {
            if value
                .as_str()
                .and_then(|text| chrono::DateTime::parse_from_rfc3339(text).ok())
                .is_none()
            {
                errors.push(format!(
                    "watermark field '{}' must be an RFC3339 timestamp",
                    watermark_field
                ));
            }
        }
    }

    if errors.is_empty() {
        Ok(())
    } else {
        Err(errors)
    }
}

async fn insert_dead_letter(
    db: &sqlx::PgPool,
    stream_id: Uuid,
    payload: Value,
    event_time: chrono::DateTime<Utc>,
    reason: &str,
    validation_errors: Vec<String>,
) -> Result<Uuid, sqlx::Error> {
    let id = Uuid::now_v7();
    sqlx::query(
        "INSERT INTO streaming_dead_letters (
             id, stream_id, payload, event_time, reason, validation_errors, status
         ) VALUES ($1, $2, $3, $4, $5, $6, 'queued')",
    )
    .bind(id)
    .bind(stream_id)
    .bind(payload)
    .bind(event_time)
    .bind(reason)
    .bind(SqlJson(validation_errors))
    .execute(db)
    .await?;
    Ok(id)
}

async fn publish_runtime_event(
    state: &AppState,
    stream_id: Uuid,
    payload: &Value,
    event_time: DateTime<Utc>,
) -> Result<i64, String> {
    let payload_bytes = serde_json::to_vec(payload).map_err(|e| e.to_string())?;
    let key = payload
        .get("id")
        .and_then(|v| v.as_str())
        .map(str::to_owned);
    state
        .hot_buffer
        .publish(stream_id, key.as_deref(), &payload_bytes)
        .await
        .map_err(|e| e.to_string())?;
    let appended = state
        .runtime_store
        .append_event(stream_id, payload.clone(), event_time)
        .await
        .map_err(|e| e.to_string())?;
    Ok(appended.sequence_no)
}

pub async fn list_streams(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
) -> ServiceResult<ListResponse<StreamDefinition>> {
    let rows = sqlx::query_as::<_, StreamRow>(
		"SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, stream_type, compression, ingest_consistency, pipeline_consistency, checkpoint_interval_ms, kind, created_at, updated_at
		 FROM streaming_streams
		 ORDER BY created_at ASC",
	)
	.fetch_all(&state.db)
	.await
	.map_err(|cause| db_error(&cause))?;

    let data: Vec<StreamDefinition> = rows
        .into_iter()
        .map(StreamDefinition::from)
        .filter(|stream| caller_allowed_for_marking(&claims, stream.default_marking.as_deref()))
        .collect();
    Ok(Json(ListResponse { data }))
}

pub async fn create_stream(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Json(payload): Json<CreateStreamRequest>,
) -> ServiceResult<StreamDefinition> {
    if !can_write_streams(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    if payload.name.trim().is_empty() {
        return Err(bad_request("stream name is required"));
    }

    let stream_id = Uuid::now_v7();
    let schema = payload.schema.unwrap_or_else(StreamSchema::default);
    let binding = payload
        .source_binding
        .unwrap_or_else(ConnectorBinding::default);
    let partitions = payload
        .partitions
        .unwrap_or(3)
        .clamp(PARTITIONS_MIN, PARTITIONS_MAX);
    let consistency = payload
        .consistency_guarantee
        .unwrap_or_else(|| "at-least-once".to_string());
    if !matches!(
        consistency.as_str(),
        "at-most-once" | "at-least-once" | "exactly-once"
    ) {
        return Err(bad_request(
            "consistency_guarantee must be one of at-most-once, at-least-once, exactly-once",
        ));
    }

    let stream_type = payload.stream_type.unwrap_or_default();
    let compression = payload.compression.unwrap_or(false);
    let ingest_consistency = payload.ingest_consistency.unwrap_or_default();
    if matches!(ingest_consistency, StreamConsistency::ExactlyOnce) {
        return Err(unprocessable(
            ERR_INGEST_EXACTLY_ONCE,
            "streaming sources only support AT_LEAST_ONCE for extracts and exports",
        ));
    }
    let pipeline_consistency = payload.pipeline_consistency.unwrap_or_default();
    let checkpoint_interval_ms = payload
        .checkpoint_interval_ms
        .unwrap_or(2_000)
        .clamp(100, 86_400_000);

    let stream_profile = payload.stream_profile.clone().unwrap_or_default();
    let schema_avro = payload.schema_avro.clone();
    let schema_fingerprint = schema_avro
        .as_ref()
        .and_then(|s| compute_avro_fingerprint(s).ok());
    let compat_mode = payload
        .schema_compatibility_mode
        .clone()
        .unwrap_or_else(|| "BACKWARD".to_string());
    let default_marking = payload.default_marking.clone();
    let kind = payload.kind.unwrap_or_default();
    let retention_hours = payload.retention_hours.unwrap_or(72);
    let schema_json: Value =
        serde_json::to_value(&schema).unwrap_or_else(|_| serde_json::json!({}));
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query(
        "INSERT INTO streaming_streams (
             id, name, description, status, schema, source_binding, retention_hours, partitions,
             consistency_guarantee, stream_profile, schema_avro, schema_fingerprint,
             schema_compatibility_mode, default_marking,
             stream_type, compression, ingest_consistency, pipeline_consistency,
             checkpoint_interval_ms, kind
         )
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)",
    )
    .bind(stream_id)
    .bind(payload.name.trim())
    .bind(payload.description.unwrap_or_default())
    .bind(payload.status.unwrap_or_else(|| "active".to_string()))
    .bind(SqlJson(schema))
    .bind(SqlJson(binding))
    .bind(retention_hours)
    .bind(partitions)
    .bind(&consistency)
    .bind(SqlJson(stream_profile.clone()))
    .bind(schema_avro.as_ref().map(SqlJson))
    .bind(schema_fingerprint.as_deref())
    .bind(&compat_mode)
    .bind(default_marking.as_deref())
    .bind(stream_type.as_str())
    .bind(compression)
    .bind(ingest_consistency.as_str())
    .bind(pipeline_consistency.as_str())
    .bind(checkpoint_interval_ms)
    .bind(kind.as_str())
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    // Seed generation-1 view so the push proxy and the History tab
    // have something to render straight away. The seed view RID
    // matches the backfill done by `20260504000002_stream_views.sql`
    // for legacy rows.
    let stream_rid = crate::models::stream_view::stream_rid_for(stream_id);
    let view_rid = crate::models::stream_view::view_rid_for(stream_id);
    let view_config = serde_json::json!({
        "stream_type":            stream_type.as_str(),
        "compression":            compression,
        "partitions":             partitions,
        "retention_hours":        retention_hours,
        "ingest_consistency":     ingest_consistency.as_str(),
        "pipeline_consistency":   pipeline_consistency.as_str(),
        "checkpoint_interval_ms": checkpoint_interval_ms,
    });
    sqlx::query(
        "INSERT INTO streaming_stream_views (
             id, stream_rid, view_rid, schema_json, config_json, generation, active, created_by
         ) VALUES ($1, $2, $3, $4, $5, 1, true, $6)
         ON CONFLICT (view_rid) DO NOTHING",
    )
    .bind(Uuid::now_v7())
    .bind(&stream_rid)
    .bind(&view_rid)
    .bind(SqlJson(schema_json))
    .bind(SqlJson(view_config))
    .bind(claims.sub.to_string())
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    // Seed schema history v1 when an Avro schema was supplied at creation
    // time so schema evolution and outbox publication stay atomic.
    if let (Some(schema), Some(fp)) = (schema_avro.as_ref(), schema_fingerprint.as_ref()) {
        insert_schema_history_row(&mut tx, stream_id, 1, schema, fp, &compat_mode, "system")
            .await
            .map_err(|cause| db_error(&cause))?;
    }

    let row = load_stream_row_tx(&mut tx, stream_id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let definition: StreamDefinition = row.into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::stream_created(&definition))
        .await
        .map_err(|cause| {
            tracing::error!(stream_id = %stream_id, error = %cause, "failed to enqueue outbox event");
            crate::handlers::internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;

    // Materialise the hot buffer topic for this stream. Errors are logged
    // but do not fail the request — the stream is already persisted and
    // the operator can `update_stream` to retry topic creation later.
    let effective_partitions = stream_profile
        .partitions
        .unwrap_or(partitions)
        .clamp(1, 1024);
    if !stream_profile.to_kafka_settings().is_empty() {
        tracing::info!(
            stream_id = %stream_id,
            high_throughput = stream_profile.high_throughput,
            compressed = stream_profile.compressed,
            partitions = effective_partitions,
            "applying stream profile to hot buffer"
        );
    }
    if let Err(err) = state
        .hot_buffer
        .ensure_topic(stream_id, effective_partitions)
        .await
    {
        tracing::warn!(
            stream_id = %stream_id,
            error = %err,
            "hot buffer ensure_topic failed; stream created without backing topic"
        );
    }
    if let Err(err) = state
        .hot_buffer
        .apply_stream_type(stream_id, stream_type, compression)
        .await
    {
        tracing::warn!(
            stream_id = %stream_id,
            error = %err,
            "hot buffer apply_stream_type failed; falling back to base producer"
        );
    }

    emit_audit_event(
        &claims,
        "streaming.stream.created",
        "streaming_stream",
        definition.id,
        serde_json::json!({
            "name": definition.name,
            "default_marking": definition.default_marking,
            "has_avro_schema": definition.schema_avro.is_some(),
        }),
    );
    Ok(Json(definition))
}

pub async fn update_stream(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
    Json(payload): Json<UpdateStreamRequest>,
) -> ServiceResult<StreamDefinition> {
    if !can_write_streams(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let existing = match load_stream_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };

    let schema = payload.schema.unwrap_or(existing.schema.0);
    let binding = payload.source_binding.unwrap_or(existing.source_binding.0);
    let new_avro = payload.schema_avro.clone();
    let new_fingerprint = new_avro
        .as_ref()
        .and_then(|s| compute_avro_fingerprint(s).ok());
    let compat_mode = payload
        .schema_compatibility_mode
        .clone()
        .unwrap_or(existing.schema_compatibility_mode.clone());
    let new_marking = payload.default_marking.clone();

    // Resolve the new column values, validating Foundry rules.
    let new_stream_type = payload
        .stream_type
        .unwrap_or_else(|| StreamType::from_str(&existing.stream_type).unwrap_or_default());
    let new_compression = payload.compression.unwrap_or(existing.compression);
    let new_ingest = payload.ingest_consistency.unwrap_or_else(|| {
        StreamConsistency::from_str(&existing.ingest_consistency).unwrap_or_default()
    });
    if matches!(new_ingest, StreamConsistency::ExactlyOnce) {
        return Err(unprocessable(
            ERR_INGEST_EXACTLY_ONCE,
            "streaming sources only support AT_LEAST_ONCE for extracts and exports",
        ));
    }
    let new_pipeline = payload.pipeline_consistency.unwrap_or_else(|| {
        StreamConsistency::from_str(&existing.pipeline_consistency).unwrap_or_default()
    });
    let new_checkpoint_interval_ms = payload
        .checkpoint_interval_ms
        .map(|v| v.clamp(100, 86_400_000))
        .unwrap_or(existing.checkpoint_interval_ms);
    let new_partitions = match payload.partitions {
        Some(requested) => {
            let clamped = requested.clamp(PARTITIONS_MIN, PARTITIONS_MAX);
            if clamped < existing.partitions {
                return Err(conflict(
                    ERR_PARTITIONS_SHRINK,
                    "Kafka does not support shrinking partitions; use Reset Stream",
                ));
            }
            clamped
        }
        None => existing.partitions,
    };

    let new_kind = payload.kind.unwrap_or_else(|| {
        crate::models::stream_view::StreamKind::from_str(&existing.kind).unwrap_or_default()
    });
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query(
        "UPDATE streaming_streams
		 SET name = $2,
		     description = $3,
		     status = $4,
		     schema = $5,
		     source_binding = $6,
		     retention_hours = $7,
		     partitions = $8,
		     consistency_guarantee = $9,
		     stream_profile = $10,
		     schema_avro = COALESCE($11, schema_avro),
		     schema_fingerprint = COALESCE($12, schema_fingerprint),
		     schema_compatibility_mode = $13,
		     default_marking = COALESCE($14, default_marking),
		     stream_type = $15,
		     compression = $16,
		     ingest_consistency = $17,
		     pipeline_consistency = $18,
		     checkpoint_interval_ms = $19,
		     kind = $20,
		     updated_at = now()
		 WHERE id = $1",
    )
    .bind(id)
    .bind(payload.name.unwrap_or(existing.name))
    .bind(payload.description.unwrap_or(existing.description))
    .bind(payload.status.unwrap_or(existing.status))
    .bind(SqlJson(schema))
    .bind(SqlJson(binding))
    .bind(payload.retention_hours.unwrap_or(existing.retention_hours))
    .bind(new_partitions)
    .bind(
        payload
            .consistency_guarantee
            .unwrap_or(existing.consistency_guarantee),
    )
    .bind(SqlJson(
        payload.stream_profile.unwrap_or(existing.stream_profile.0),
    ))
    .bind(new_avro.as_ref().map(SqlJson))
    .bind(new_fingerprint.as_deref())
    .bind(&compat_mode)
    .bind(new_marking.as_deref())
    .bind(new_stream_type.as_str())
    .bind(new_compression)
    .bind(new_ingest.as_str())
    .bind(new_pipeline.as_str())
    .bind(new_checkpoint_interval_ms)
    .bind(new_kind.as_str())
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    // If a fresh Avro schema was provided, append it to history (next
    // version after the current max).
    if let (Some(schema), Some(fp)) = (new_avro.as_ref(), new_fingerprint.as_ref()) {
        let next_version: i32 = sqlx::query_scalar(
            "SELECT COALESCE(MAX(version), 0) + 1 FROM streaming_stream_schema_history WHERE stream_id = $1",
        )
        .bind(id)
        .fetch_one(&mut *tx)
        .await
        .map_err(|cause| db_error(&cause))?;
        insert_schema_history_row(
            &mut tx,
            id,
            next_version,
            schema,
            fp,
            &compat_mode,
            "operator",
        )
        .await
        .map_err(|cause| db_error(&cause))?;
    }

    let row = load_stream_row_tx(&mut tx, id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let definition: StreamDefinition = row.into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::stream_updated(&definition))
        .await
        .map_err(|cause| {
            tracing::error!(stream_id = %id, error = %cause, "failed to enqueue outbox event");
            crate::handlers::internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;

    // Reapply Kafka producer tuning for the new stream type and grow
    // the topic to the new partition count when the producer supports
    // it. Both calls are best-effort: failures are logged and the API
    // request still succeeds because the metadata is already persisted.
    if let Err(err) = state
        .hot_buffer
        .ensure_topic(definition.id, new_partitions)
        .await
    {
        tracing::warn!(
            stream_id = %definition.id,
            error = %err,
            "hot buffer ensure_topic failed during update_stream"
        );
    }
    if let Err(err) = state
        .hot_buffer
        .apply_stream_type(definition.id, new_stream_type, new_compression)
        .await
    {
        tracing::warn!(
            stream_id = %definition.id,
            error = %err,
            "hot buffer apply_stream_type failed during update_stream"
        );
    }

    emit_audit_event(
        &claims,
        "streaming.stream.updated",
        "streaming_stream",
        definition.id,
        serde_json::json!({ "name": definition.name }),
    );
    Ok(Json(definition))
}

/// `GET /streams/{id}/config` — projection of the persisted stream into
/// the Foundry-parity [`StreamConfig`] view.
pub async fn get_stream_config(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
) -> ServiceResult<StreamConfig> {
    let row = match load_stream_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let definition: StreamDefinition = row.into();
    if !caller_allowed_for_marking(&claims, definition.default_marking.as_deref()) {
        return Err(forbidden("caller does not have clearance for this stream"));
    }
    Ok(Json(definition.config_view()))
}

/// `PUT /streams/{id}/config` — atomic patch of the StreamConfig fields
/// only (type / compression / partitions / retention / consistency /
/// checkpoint cadence). Mirrors the Foundry "Stream Settings" modal.
pub async fn update_stream_config(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
    Json(payload): Json<UpdateStreamConfigRequest>,
) -> ServiceResult<StreamConfig> {
    if !can_write_streams(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let existing = match load_stream_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let existing_def: StreamDefinition = existing.clone().into();
    if !caller_allowed_for_marking(&claims, existing_def.default_marking.as_deref()) {
        return Err(forbidden("caller does not have clearance for this stream"));
    }

    let new_stream_type = payload.stream_type.unwrap_or(existing_def.stream_type);
    let new_compression = payload.compression.unwrap_or(existing_def.compression);
    let new_ingest = payload
        .ingest_consistency
        .unwrap_or(existing_def.ingest_consistency);
    if matches!(new_ingest, StreamConsistency::ExactlyOnce) {
        return Err(unprocessable(
            ERR_INGEST_EXACTLY_ONCE,
            "streaming sources only support AT_LEAST_ONCE for extracts and exports",
        ));
    }
    let new_pipeline = payload
        .pipeline_consistency
        .unwrap_or(existing_def.pipeline_consistency);
    let new_checkpoint_interval_ms = payload
        .checkpoint_interval_ms
        .map(|v| v.clamp(100, 86_400_000))
        .unwrap_or(existing_def.checkpoint_interval_ms);

    let new_partitions = match payload.partitions {
        Some(requested) => {
            if !(PARTITIONS_MIN..=PARTITIONS_MAX).contains(&requested) {
                return Err(bad_request(format!(
                    "partitions must be between {PARTITIONS_MIN} and {PARTITIONS_MAX} (~5 MB/s per partition)"
                )));
            }
            if requested < existing_def.partitions {
                return Err(conflict(
                    ERR_PARTITIONS_SHRINK,
                    "Kafka does not support shrinking partitions; use Reset Stream",
                ));
            }
            requested
        }
        None => existing_def.partitions,
    };

    let new_retention_hours = match payload.retention_ms {
        Some(ms) if ms > 0 => {
            // Clamp the conversion the same way the model does: hours
            // is the source of truth so we round to whole hours.
            let hours = (ms / 3_600_000).max(1).min(i64::from(i32::MAX)) as i32;
            hours
        }
        _ => existing_def.retention_hours,
    };

    let _ = STREAM_TYPE_VALUES; // referenced from CHECK error message

    sqlx::query(
        "UPDATE streaming_streams
         SET stream_type = $2,
             compression = $3,
             partitions = $4,
             retention_hours = $5,
             ingest_consistency = $6,
             pipeline_consistency = $7,
             checkpoint_interval_ms = $8,
             updated_at = now()
         WHERE id = $1",
    )
    .bind(id)
    .bind(new_stream_type.as_str())
    .bind(new_compression)
    .bind(new_partitions)
    .bind(new_retention_hours)
    .bind(new_ingest.as_str())
    .bind(new_pipeline.as_str())
    .bind(new_checkpoint_interval_ms)
    .execute(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let row = load_stream_row(&state.db, id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let definition: StreamDefinition = row.into();

    if let Err(err) = state
        .hot_buffer
        .ensure_topic(definition.id, definition.partitions)
        .await
    {
        tracing::warn!(
            stream_id = %definition.id,
            error = %err,
            "hot buffer ensure_topic failed during update_stream_config"
        );
    }
    if let Err(err) = state
        .hot_buffer
        .apply_stream_type(
            definition.id,
            definition.stream_type,
            definition.compression,
        )
        .await
    {
        tracing::warn!(
            stream_id = %definition.id,
            error = %err,
            "hot buffer apply_stream_type failed during update_stream_config"
        );
    }

    emit_audit_event(
        &claims,
        "streaming.stream.config.updated",
        "streaming_stream",
        definition.id,
        serde_json::json!({
            "stream_type": definition.stream_type.as_str(),
            "compression": definition.compression,
            "partitions": definition.partitions,
            "ingest_consistency": definition.ingest_consistency.as_str(),
            "pipeline_consistency": definition.pipeline_consistency.as_str(),
            "checkpoint_interval_ms": definition.checkpoint_interval_ms,
        }),
    );

    Ok(Json(definition.config_view()))
}

pub async fn list_windows(
    axum::extract::State(state): axum::extract::State<AppState>,
) -> ServiceResult<ListResponse<WindowDefinition>> {
    let rows = sqlx::query_as::<_, WindowRow>(
        "SELECT id, name, description, status, window_type, duration_seconds, slide_seconds,
		        session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields,
		        keyed, key_columns, state_ttl_seconds,
		        created_at, updated_at
		 FROM streaming_windows
		 ORDER BY created_at ASC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(ListResponse {
        data: rows.into_iter().map(Into::into).collect(),
    }))
}

pub async fn create_window(
    axum::extract::State(state): axum::extract::State<AppState>,
    Json(payload): Json<CreateWindowRequest>,
) -> ServiceResult<WindowDefinition> {
    if payload.name.trim().is_empty() {
        return Err(bad_request("window name is required"));
    }

    let window_id = Uuid::now_v7();
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;

    let keyed = payload.keyed.unwrap_or(false);
    let key_columns = payload.key_columns.clone().unwrap_or_default();
    let state_ttl_seconds = payload.state_ttl_seconds.unwrap_or(0).clamp(0, 31_536_000);
    sqlx::query(
        "INSERT INTO streaming_windows (
		    id, name, description, status, window_type, duration_seconds, slide_seconds,
		    session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields,
		    keyed, key_columns, state_ttl_seconds
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)",
    )
    .bind(window_id)
    .bind(payload.name.trim())
    .bind(payload.description.unwrap_or_default())
    .bind(payload.status.unwrap_or_else(|| "active".to_string()))
    .bind(
        payload
            .window_type
            .unwrap_or_else(|| "tumbling".to_string()),
    )
    .bind(payload.duration_seconds.unwrap_or(300))
    .bind(payload.slide_seconds.unwrap_or(300))
    .bind(payload.session_gap_seconds.unwrap_or(180))
    .bind(payload.allowed_lateness_seconds.unwrap_or(30))
    .bind(SqlJson(payload.aggregation_keys))
    .bind(SqlJson(payload.measure_fields))
    .bind(keyed)
    .bind(SqlJson(&key_columns))
    .bind(state_ttl_seconds)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    let row = load_window_row_tx(&mut tx, window_id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let definition: WindowDefinition = row.into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::window_created(&definition))
        .await
        .map_err(|cause| {
            tracing::error!(window_id = %window_id, error = %cause, "failed to enqueue outbox event");
            crate::handlers::internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;

    Ok(Json(definition))
}

pub async fn update_window(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(id): Path<Uuid>,
    Json(payload): Json<UpdateWindowRequest>,
) -> ServiceResult<WindowDefinition> {
    let existing = match load_window_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("window not found")),
        Err(cause) => return Err(db_error(&cause)),
    };

    let new_keyed = payload.keyed.unwrap_or(existing.keyed);
    let new_key_columns = payload.key_columns.unwrap_or(existing.key_columns.0);
    let new_state_ttl = payload
        .state_ttl_seconds
        .map(|v| v.clamp(0, 31_536_000))
        .unwrap_or(existing.state_ttl_seconds);

    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query(
        "UPDATE streaming_windows
		 SET name = $2,
		     description = $3,
		     status = $4,
		     window_type = $5,
		     duration_seconds = $6,
		     slide_seconds = $7,
		     session_gap_seconds = $8,
		     allowed_lateness_seconds = $9,
		     aggregation_keys = $10,
		     measure_fields = $11,
		     keyed = $12,
		     key_columns = $13,
		     state_ttl_seconds = $14,
		     updated_at = now()
		 WHERE id = $1",
    )
    .bind(id)
    .bind(payload.name.unwrap_or(existing.name))
    .bind(payload.description.unwrap_or(existing.description))
    .bind(payload.status.unwrap_or(existing.status))
    .bind(payload.window_type.unwrap_or(existing.window_type))
    .bind(
        payload
            .duration_seconds
            .unwrap_or(existing.duration_seconds),
    )
    .bind(payload.slide_seconds.unwrap_or(existing.slide_seconds))
    .bind(
        payload
            .session_gap_seconds
            .unwrap_or(existing.session_gap_seconds),
    )
    .bind(
        payload
            .allowed_lateness_seconds
            .unwrap_or(existing.allowed_lateness_seconds),
    )
    .bind(SqlJson(
        payload
            .aggregation_keys
            .unwrap_or(existing.aggregation_keys.0),
    ))
    .bind(SqlJson(
        payload.measure_fields.unwrap_or(existing.measure_fields.0),
    ))
    .bind(new_keyed)
    .bind(SqlJson(&new_key_columns))
    .bind(new_state_ttl)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    let row = load_window_row_tx(&mut tx, id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let definition: WindowDefinition = row.into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::window_updated(&definition))
        .await
        .map_err(|cause| {
            tracing::error!(window_id = %id, error = %cause, "failed to enqueue outbox event");
            crate::handlers::internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;

    Ok(Json(definition))
}

pub async fn push_events(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(stream_id): Path<Uuid>,
    Json(payload): Json<PushStreamEventsRequest>,
) -> ServiceResult<PushStreamEventsResponse> {
    let PushStreamEventsRequest { events } = payload;

    if events.is_empty() {
        return Err(bad_request("at least one event is required"));
    }

    let stream = match load_stream_row(&state.db, stream_id).await {
        Ok(stream) => stream,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };

    if !caller_allowed_for_marking(&claims, stream.default_marking.as_deref()) {
        return Err(forbidden("caller does not have clearance for this stream"));
    }
    if !can_write_streams(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }

    // Pre-parse the Avro schema once when present — avoids re-parsing per
    // event in the hot loop below. We hand the raw schema text to
    // `event_bus_control::schema_registry::validate_payload` for each
    // event.
    let avro_text: Option<String> = stream
        .schema_avro
        .as_ref()
        .and_then(|s| serde_json::to_string(&s.0).ok());

    let mut first_sequence_no = None;
    let mut last_sequence_no = None;
    let mut accepted_events = 0usize;
    let mut dead_lettered_events = 0usize;
    for event in events {
        let event_time = event
            .event_time
            .or_else(|| {
                event
                    .payload
                    .get("event_time")
                    .and_then(|value| value.as_str())
                    .and_then(|value| chrono::DateTime::parse_from_rfc3339(value).ok())
                    .map(|value| value.with_timezone(&Utc))
            })
            .unwrap_or_else(Utc::now);

        if let Err(validation_errors) =
            validate_event_against_schema(&stream.schema.0, &event.payload, event_time)
        {
            insert_dead_letter(
                &state.db,
                stream_id,
                event.payload,
                event_time,
                "schema_validation_failed",
                validation_errors,
            )
            .await
            .map_err(|cause| db_error(&cause))?;
            dead_lettered_events += 1;
            state
                .metrics
                .record_dead_letter(&stream.name, "schema_validation_failed");
            continue;
        }

        // Bloque E2: Avro validation gate. When the stream has an Avro
        // schema attached we additionally validate the payload against it.
        if let Some(text) = avro_text.as_deref() {
            if let Err(err) =
                schema_registry::validate_payload(SchemaType::Avro, text, &event.payload)
            {
                insert_dead_letter(
                    &state.db,
                    stream_id,
                    event.payload,
                    event_time,
                    "avro_validation_failed",
                    vec![err.to_string()],
                )
                .await
                .map_err(|cause| db_error(&cause))?;
                dead_lettered_events += 1;
                state
                    .metrics
                    .record_dead_letter(&stream.name, "avro_validation_failed");
                continue;
            }
        }

        let sequence_no =
            match publish_runtime_event(&state, stream_id, &event.payload, event_time).await {
                Ok(sequence_no) => sequence_no,
                Err(err) => {
                    tracing::warn!(
                        stream_id = %stream_id,
                        error = %err,
                        "hot buffer publish failed; recording dead-letter"
                    );
                    insert_dead_letter(
                        &state.db,
                        stream_id,
                        event.payload,
                        event_time,
                        "hot_buffer_publish_failed",
                        vec![err.to_string()],
                    )
                    .await
                    .map_err(|cause| db_error(&cause))?;
                    dead_lettered_events += 1;
                    state
                        .metrics
                        .record_dead_letter(&stream.name, "hot_buffer_publish_failed");
                    continue;
                }
            };

        accepted_events += 1;
        if first_sequence_no.is_none() {
            first_sequence_no = Some(sequence_no);
        }
        last_sequence_no = Some(sequence_no);
    }

    state
        .metrics
        .record_stream_rows_in(&stream.name, accepted_events as u64);
    // Bloque P4 — `streaming_records_ingested_total` is the canonical
    // metric stream monitors evaluate against the
    // `INGEST_RECORDS` rule.
    state
        .metrics
        .record_ingest(&stream.name, accepted_events as u64);

    Ok(Json(PushStreamEventsResponse {
        stream_id,
        accepted_events,
        dead_lettered_events,
        first_sequence_no,
        last_sequence_no,
    }))
}

pub async fn list_dead_letters(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(stream_id): Path<Uuid>,
) -> ServiceResult<ListResponse<StreamingDeadLetter>> {
    match load_stream_row(&state.db, stream_id).await {
        Ok(_) => {}
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    }

    let rows = sqlx::query_as::<_, StreamingDeadLetterRow>(
        "SELECT id, stream_id, payload, event_time, reason, validation_errors, status,
                replay_count, last_replayed_at, created_at, updated_at
         FROM streaming_dead_letters
         WHERE stream_id = $1
         ORDER BY created_at DESC",
    )
    .bind(stream_id)
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(ListResponse {
        data: rows.into_iter().map(Into::into).collect(),
    }))
}

pub async fn replay_dead_letter(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(id): Path<Uuid>,
    Json(payload): Json<ReplayDeadLetterRequest>,
) -> ServiceResult<ReplayDeadLetterResponse> {
    let dead_letter = match load_dead_letter_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("dead letter not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let stream = match load_stream_row(&state.db, dead_letter.stream_id).await {
        Ok(stream) => stream,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };

    let next_payload = payload.payload.unwrap_or(dead_letter.payload.clone());
    let next_event_time = payload.event_time.unwrap_or(dead_letter.event_time);
    if let Err(errors) =
        validate_event_against_schema(&stream.schema.0, &next_payload, next_event_time)
    {
        return Err(bad_request(format!(
            "dead letter replay still violates schema: {}",
            errors.join("; ")
        )));
    }

    let sequence_no = publish_runtime_event(
        &state,
        dead_letter.stream_id,
        &next_payload,
        next_event_time,
    )
    .await
    .map_err(|cause| {
        bad_request(format!(
            "dead letter replay failed to reach hot buffer: {cause}"
        ))
    })?;

    sqlx::query(
        "UPDATE streaming_dead_letters
         SET payload = $2,
             event_time = $3,
             status = 'replayed',
             replay_count = replay_count + 1,
             last_replayed_at = now(),
             validation_errors = '[]'::jsonb,
             updated_at = now()
         WHERE id = $1",
    )
    .bind(id)
    .bind(next_payload)
    .bind(next_event_time)
    .execute(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let dead_letter = load_dead_letter_row(&state.db, id)
        .await
        .map_err(|cause| db_error(&cause))?;

    Ok(Json(ReplayDeadLetterResponse {
        dead_letter: dead_letter.into(),
        replay_sequence_no: sequence_no,
    }))
}

/// Query parameters for `GET /streams/{id}/read`.
///
/// `from`/`to` are inclusive ISO-8601 timestamps. `limit` caps the number
/// of merged rows returned (max 10_000).
#[derive(Debug, Deserialize)]
pub struct ReadStreamQuery {
    #[serde(default)]
    pub from: Option<DateTime<Utc>>,
    #[serde(default)]
    pub to: Option<DateTime<Utc>>,
    #[serde(default)]
    pub limit: Option<i64>,
}

/// Single row returned by [`read_stream`]. `source` distinguishes hot
/// (live Postgres rows) from cold (rows materialised by the archiver
/// into Iceberg/Parquet snapshots).
#[derive(Debug, serde::Serialize)]
pub struct ReadStreamRow {
    pub sequence_no: Option<i64>,
    pub event_time: DateTime<Utc>,
    pub payload: Value,
    pub source: &'static str,
    pub snapshot_id: Option<String>,
    pub parquet_path: Option<String>,
}

/// Hot+cold merged read endpoint.
///
/// Strategy:
///   1. Compute `cold_watermark` from the runtime store. Anything older
///      than that watermark is guaranteed to be available in cold.
///   2. Query the hot runtime store tagged `source = "hot"` filtered by
///      `from`/`to`.
///   3. If `from < cold_watermark`, also list matching cold-tier
///      snapshots (metadata + path) so callers can stream them out of
///      band — Postgres is not the right place to load Parquet files,
///      and the dataset writer (legacy or Iceberg) is the source of
///      truth for the actual bytes.
///   4. Merge by `event_time` ascending and apply the `limit` cap.
pub async fn read_stream(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(stream_id): Path<Uuid>,
    Query(params): Query<ReadStreamQuery>,
) -> ServiceResult<ListResponse<ReadStreamRow>> {
    match load_stream_row(&state.db, stream_id).await {
        Ok(_) => {}
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    }

    let limit = params.limit.unwrap_or(1_000).clamp(1, 10_000);
    let from = params
        .from
        .unwrap_or_else(|| Utc::now() - chrono::Duration::hours(24));
    let to = params.to.unwrap_or_else(Utc::now);
    if from >= to {
        return Err(bad_request("`from` must be strictly before `to`"));
    }

    let cold_watermark = state
        .runtime_store
        .cold_watermark(stream_id)
        .await
        .map_err(|cause| bad_request(cause.to_string()))?;

    let mut merged: Vec<ReadStreamRow> = Vec::with_capacity(limit as usize);

    let hot_rows = state
        .runtime_store
        .list_events_between(stream_id, from, to, limit as usize)
        .await
        .map_err(|cause| bad_request(cause.to_string()))?;
    for row in hot_rows {
        merged.push(ReadStreamRow {
            sequence_no: Some(row.sequence_no),
            event_time: row.event_time,
            payload: row.payload,
            source: "hot",
            snapshot_id: None,
            parquet_path: None,
        });
    }

    if cold_watermark.map(|w| from < w).unwrap_or(false) {
        let cold_rows = state
            .runtime_store
            .list_cold_archives(stream_id, from, to, limit as usize)
            .await
            .map_err(|cause| bad_request(cause.to_string()))?;
        for row in cold_rows {
            merged.push(ReadStreamRow {
                sequence_no: None,
                event_time: row.snapshot_at,
                payload: serde_json::json!({
                    "row_count": row.row_count,
                    "hint": "fetch parquet file at parquet_path for raw rows",
                }),
                source: "cold",
                snapshot_id: Some(row.snapshot_id),
                parquet_path: Some(row.parquet_path),
            });
        }
    }

    merged.sort_by_key(|r| r.event_time);
    merged.truncate(limit as usize);

    Ok(Json(ListResponse { data: merged }))
}

// ---------------------------------------------------------------------
// Bloque P5 — `GET /streams/{id}/preview` hybrid hot/cold view
// ---------------------------------------------------------------------

/// `from` selector for the preview endpoint.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PreviewMode {
    /// Read N records from the cold archive first; if N is not met,
    /// complement with hot-buffer ascending.
    Oldest,
    /// Hot buffer only (live records).
    HotOnly,
    /// Cold archive only (Iceberg / Parquet pointers).
    ColdOnly,
}

impl Default for PreviewMode {
    fn default() -> Self {
        Self::Oldest
    }
}

#[derive(Debug, Default, Deserialize)]
pub struct PreviewQuery {
    #[serde(default)]
    pub from: Option<PreviewMode>,
    #[serde(default)]
    pub limit: Option<i64>,
    /// Optional cursor for hot-only paging (sequence_no).
    #[serde(default)]
    pub from_offset: Option<i64>,
}

#[derive(Debug, serde::Serialize)]
pub struct PreviewRow {
    pub sequence_no: Option<i64>,
    pub event_time: DateTime<Utc>,
    pub payload: Value,
    /// Per-record source label so the UI can render a "live" /
    /// "archived" badge (Bloque P5 LiveDataView).
    pub source: &'static str,
    pub snapshot_id: Option<String>,
    pub parquet_path: Option<String>,
}

#[derive(Debug, serde::Serialize)]
pub struct PreviewResponse {
    /// `hot` / `cold` / `hybrid` — describes which tiers actually
    /// contributed to the response so the UI knows whether to bother
    /// rendering the "View" toggles.
    pub source: &'static str,
    pub data: Vec<PreviewRow>,
}

/// `GET /streams/{id}/preview?from=oldest|hot_only|cold_only&limit=N&from_offset=...`
///
/// The Foundry "Recent" / "Live" / "Historical" tabs in the
/// LiveDataView component are the primary callers. The handler
/// returns `{source, data: [...]}` where every row carries its own
/// `source: "hot" | "cold"` label.
pub async fn preview_stream(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(stream_id): Path<Uuid>,
    Query(params): Query<PreviewQuery>,
) -> ServiceResult<PreviewResponse> {
    match load_stream_row(&state.db, stream_id).await {
        Ok(_) => {}
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    }

    let limit = params.limit.unwrap_or(50).clamp(1, 10_000);
    let mode = params.from.unwrap_or_default();

    // Hot windows for the `oldest` / `hot_only` branches.
    let now = Utc::now();
    let hot_from = now - chrono::Duration::hours(24);

    let mut data: Vec<PreviewRow> = Vec::new();
    let mut had_hot = false;
    let mut had_cold = false;

    match mode {
        PreviewMode::HotOnly => {
            let hot_rows = state
                .runtime_store
                .list_events_between(stream_id, hot_from, now, limit as usize)
                .await
                .map_err(|cause| bad_request(cause.to_string()))?;
            for row in hot_rows {
                if let Some(off) = params.from_offset {
                    if row.sequence_no <= off {
                        continue;
                    }
                }
                had_hot = true;
                data.push(PreviewRow {
                    sequence_no: Some(row.sequence_no),
                    event_time: row.event_time,
                    payload: row.payload,
                    source: "hot",
                    snapshot_id: None,
                    parquet_path: None,
                });
            }
        }
        PreviewMode::ColdOnly => {
            let cold_rows = state
                .runtime_store
                .list_cold_archives(
                    stream_id,
                    chrono::DateTime::<Utc>::from_timestamp(0, 0).unwrap(),
                    now,
                    limit as usize,
                )
                .await
                .map_err(|cause| bad_request(cause.to_string()))?;
            for row in cold_rows {
                had_cold = true;
                data.push(PreviewRow {
                    sequence_no: None,
                    event_time: row.snapshot_at,
                    payload: serde_json::json!({
                        "row_count": row.row_count,
                        "hint": "fetch parquet file at parquet_path",
                    }),
                    source: "cold",
                    snapshot_id: Some(row.snapshot_id),
                    parquet_path: Some(row.parquet_path),
                });
            }
        }
        PreviewMode::Oldest => {
            // 1. Cold archive ascending — Iceberg/Parquet pointers
            //    sorted by snapshot time.
            let cold_rows = state
                .runtime_store
                .list_cold_archives(
                    stream_id,
                    chrono::DateTime::<Utc>::from_timestamp(0, 0).unwrap(),
                    now,
                    limit as usize,
                )
                .await
                .map_err(|cause| bad_request(cause.to_string()))?;
            for row in cold_rows {
                had_cold = true;
                data.push(PreviewRow {
                    sequence_no: None,
                    event_time: row.snapshot_at,
                    payload: serde_json::json!({
                        "row_count": row.row_count,
                        "hint": "fetch parquet file at parquet_path",
                    }),
                    source: "cold",
                    snapshot_id: Some(row.snapshot_id),
                    parquet_path: Some(row.parquet_path),
                });
            }
            data.sort_by_key(|r| r.event_time);
            data.truncate(limit as usize);
            // 2. Complement from the hot buffer if we still have
            //    budget. The hot rows are appended after sorting so
            //    the response stays "oldest first".
            if data.len() < limit as usize {
                let remaining = limit as usize - data.len();
                let hot_rows = state
                    .runtime_store
                    .list_events_between(stream_id, hot_from, now, remaining)
                    .await
                    .map_err(|cause| bad_request(cause.to_string()))?;
                for row in hot_rows {
                    had_hot = true;
                    data.push(PreviewRow {
                        sequence_no: Some(row.sequence_no),
                        event_time: row.event_time,
                        payload: row.payload,
                        source: "hot",
                        snapshot_id: None,
                        parquet_path: None,
                    });
                }
            }
        }
    }

    let source = match (had_hot, had_cold) {
        (true, true) => "hybrid",
        (true, false) => "hot",
        (false, true) => "cold",
        (false, false) => "hot", // empty response — caller still got a hot-only window.
    };
    Ok(Json(PreviewResponse { source, data }))
}

// ---------------------------------------------------------------------
// Bloque P4 — `GET /streams/{id}/metrics?window=...`
// ---------------------------------------------------------------------

/// Per-stream rollup the monitoring evaluator queries.
///
/// `window` accepts `5m`, `30m`, `<seconds>s` or a bare integer
/// (interpreted as seconds). Defaults to 5 minutes when omitted.
#[derive(Debug, Default, Deserialize)]
pub struct StreamMetricsQuery {
    pub window: Option<String>,
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct StreamMetricsResponse {
    pub stream_id: Uuid,
    pub window_seconds: i64,
    pub records_ingested: f64,
    pub records_output: f64,
    pub total_lag: f64,
    pub total_throughput: f64,
    pub utilization_pct: f64,
    pub from: DateTime<Utc>,
    pub to: DateTime<Utc>,
}

/// Parse a Foundry-style window string (`5m`, `30m`, `<n>s`, bare
/// integer = seconds, also accepts `<n>h`). Returns the value in
/// seconds, clamped to the documented `[60, 86400]` range.
pub fn parse_window(value: Option<&str>) -> Result<i64, String> {
    let raw = value.unwrap_or("5m").trim();
    if raw.is_empty() {
        return Err("window must not be empty".into());
    }
    let (num_part, unit_seconds): (&str, i64) = if let Some(prefix) = raw.strip_suffix("ms") {
        // milliseconds — round down to whole seconds.
        let ms: i64 = prefix
            .parse()
            .map_err(|_| format!("invalid window: {raw}"))?;
        return clamp_window_seconds(ms / 1000);
    } else if let Some(prefix) = raw.strip_suffix('s') {
        (prefix, 1)
    } else if let Some(prefix) = raw.strip_suffix('m') {
        (prefix, 60)
    } else if let Some(prefix) = raw.strip_suffix('h') {
        (prefix, 3600)
    } else {
        (raw, 1)
    };
    let n: i64 = num_part
        .parse()
        .map_err(|_| format!("invalid window: {raw}"))?;
    clamp_window_seconds(n.saturating_mul(unit_seconds))
}

fn clamp_window_seconds(total: i64) -> Result<i64, String> {
    if !(60..=86_400).contains(&total) {
        return Err(format!("window must resolve to 60s..86400s (got {total}s)"));
    }
    Ok(total)
}

/// `GET /api/v1/streaming/streams/{id}/metrics?window=5m|30m|<n>s`
pub async fn get_stream_metrics(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(stream_id): Path<Uuid>,
    Query(params): Query<StreamMetricsQuery>,
) -> ServiceResult<StreamMetricsResponse> {
    // Confirm the stream exists so callers get 404 instead of zeros
    // when they typo the rid.
    let stream_row = match load_stream_row(&state.db, stream_id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("stream not found")),
        Err(cause) => return Err(db_error(&cause)),
    };

    let window_seconds = parse_window(params.window.as_deref()).map_err(bad_request)?;
    let to = Utc::now();
    let from = to - chrono::Duration::seconds(window_seconds);

    let events = state
        .runtime_store
        .list_events_between(stream_id, from, to, 100_000)
        .await
        .map_err(|cause| crate::handlers::internal_error(cause.to_string()))?;
    let records_ingested = events.len() as f64;

    // Per-second throughput averaged across the window. The
    // monitoring evaluator can compare this against a rate threshold.
    let total_throughput = records_ingested / window_seconds as f64;

    // Output records / lag come from the latest topology run that
    // consumes this stream. We pick the row whose
    // `source_stream_ids` JSON array contains our stream's UUID
    // and aggregate across the matches; if no run is present,
    // both fields default to zero so monitors don't fire on cold
    // installations.
    #[derive(sqlx::FromRow)]
    struct TopRow {
        out: i64,
        lag_ms: i64,
    }
    let rows: Vec<TopRow> = sqlx::query_as::<_, TopRow>(
        "SELECT
            COALESCE(((r.metrics)->>'output_events')::bigint, 0) AS out,
            COALESCE(((r.backpressure_snapshot)->>'lag_ms')::bigint, 0) AS lag_ms
           FROM streaming_topology_runs r
           JOIN streaming_topologies t ON t.id = r.topology_id
          WHERE t.source_stream_ids @> jsonb_build_array($1::text)::jsonb
          ORDER BY r.created_at DESC
          LIMIT 16",
    )
    .bind(stream_id.to_string())
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();
    let records_output = rows.iter().map(|r| r.out as f64).sum::<f64>();
    let total_lag = rows.iter().map(|r| r.lag_ms as f64).fold(0.0_f64, f64::max);
    // The Foundry-default capacity heuristic is `5 MB/s per partition`
    // (see Streams.md). We don't have a byte-level meter so we
    // approximate utilization as
    //   throughput_records / (partitions * 5000 records/s)
    // which gives a rough 0..1 value the UI can render as a
    // percentage.
    let nominal_capacity = (stream_row.partitions as f64 * 5000.0).max(1.0);
    let utilization_pct = (total_throughput / nominal_capacity).clamp(0.0, 1.0);

    state
        .metrics
        .set_utilization(&stream_row.name, utilization_pct);

    Ok(Json(StreamMetricsResponse {
        stream_id,
        window_seconds,
        records_ingested,
        records_output,
        total_lag,
        total_throughput,
        utilization_pct,
        from,
        to,
    }))
}

#[cfg(test)]
mod tests {
    use chrono::Utc;
    use serde_json::json;

    use crate::models::stream::{StreamField, StreamSchema};

    use super::validate_event_against_schema;

    fn sample_schema() -> StreamSchema {
        StreamSchema {
            fields: vec![
                StreamField {
                    name: "event_time".to_string(),
                    data_type: "timestamp".to_string(),
                    nullable: false,
                    semantic_role: "event_time".to_string(),
                },
                StreamField {
                    name: "customer_id".to_string(),
                    data_type: "string".to_string(),
                    nullable: false,
                    semantic_role: "join_key".to_string(),
                },
                StreamField {
                    name: "amount".to_string(),
                    data_type: "float".to_string(),
                    nullable: false,
                    semantic_role: "measure".to_string(),
                },
            ],
            primary_key: Some("customer_id".to_string()),
            watermark_field: Some("event_time".to_string()),
        }
    }

    #[test]
    fn rejects_invalid_stream_payloads_into_dlq_path() {
        let result = validate_event_against_schema(
            &sample_schema(),
            &json!({
                "customer_id": 42,
                "amount": "high"
            }),
            Utc::now(),
        );

        let errors = result.expect_err("payload should fail schema validation");
        assert!(errors.iter().any(|error| error.contains("customer_id")));
        assert!(errors.iter().any(|error| error.contains("amount")));
    }

    #[test]
    fn accepts_valid_payloads_with_rfc3339_watermark() {
        validate_event_against_schema(
            &sample_schema(),
            &json!({
                "event_time": "2026-04-25T10:15:00Z",
                "customer_id": "C-100",
                "amount": 87.5
            }),
            Utc::now(),
        )
        .expect("payload should pass schema validation");
    }
}
