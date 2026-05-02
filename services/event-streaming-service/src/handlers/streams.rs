use axum::{Extension, Json, extract::{Path, Query}};
use auth_middleware::claims::Claims;
use chrono::{DateTime, Utc};
use event_bus_control::schema_registry::{self, SchemaType};
use serde::Deserialize;
use serde_json::Value;
use sqlx::types::Json as SqlJson;
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, forbidden, not_found},
    models::{
        ListResponse,
        dead_letter::{
            ReplayDeadLetterRequest, ReplayDeadLetterResponse, StreamingDeadLetter,
            StreamingDeadLetterRow,
        },
        stream::{
            ConnectorBinding, CreateStreamRequest, PushStreamEventsRequest,
            PushStreamEventsResponse, StreamDefinition, StreamRow, StreamSchema,
            UpdateStreamRequest,
        },
        window::{CreateWindowRequest, UpdateWindowRequest, WindowDefinition, WindowRow},
    },
};

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
    db: &sqlx::PgPool,
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
    .execute(db)
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
		"SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, created_at, updated_at
		 FROM streaming_streams
		 WHERE id = $1",
	)
	.bind(id)
	.fetch_one(db)
	.await
}

async fn load_window_row(db: &sqlx::PgPool, id: Uuid) -> Result<WindowRow, sqlx::Error> {
    sqlx::query_as::<_, WindowRow>(
        "SELECT id, name, description, status, window_type, duration_seconds, slide_seconds,
		        session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields,
		        created_at, updated_at
		 FROM streaming_windows
		 WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
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

pub async fn list_streams(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
) -> ServiceResult<ListResponse<StreamDefinition>> {
    let rows = sqlx::query_as::<_, StreamRow>(
		"SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, created_at, updated_at
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
    let partitions = payload.partitions.unwrap_or(3).clamp(1, 1024);
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
    sqlx::query(
		"INSERT INTO streaming_streams (id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)",
	)
	.bind(stream_id)
	.bind(payload.name.trim())
	.bind(payload.description.unwrap_or_default())
	.bind(payload.status.unwrap_or_else(|| "active".to_string()))
	.bind(SqlJson(schema))
	.bind(SqlJson(binding))
	.bind(payload.retention_hours.unwrap_or(72))
	.bind(partitions)
	.bind(&consistency)
	.bind(SqlJson(stream_profile.clone()))
	.bind(schema_avro.as_ref().map(SqlJson))
	.bind(schema_fingerprint.as_deref())
	.bind(&compat_mode)
	.bind(default_marking.as_deref())
	.execute(&state.db)
	.await
	.map_err(|cause| db_error(&cause))?;

    // Seed schema history v1 when an Avro schema was supplied at creation
    // time. Failures here only emit a warning — the stream itself is
    // already persisted.
    if let (Some(schema), Some(fp)) = (schema_avro.as_ref(), schema_fingerprint.as_ref()) {
        let _ = insert_schema_history_row(
            &state.db,
            stream_id,
            1,
            schema,
            fp,
            &compat_mode,
            "system",
        )
        .await
        .map_err(|err| {
            tracing::warn!(stream = %stream_id, error = %err, "failed to seed schema history")
        });
    }

    // Materialise the hot buffer topic for this stream. Errors are logged
    // but do not fail the request — the stream is already persisted and
    // the operator can `update_stream` to retry topic creation later.
    let effective_partitions = stream_profile.partitions.unwrap_or(partitions).clamp(1, 1024);
    if !stream_profile.to_kafka_settings().is_empty() {
        tracing::info!(
            stream_id = %stream_id,
            high_throughput = stream_profile.high_throughput,
            compressed = stream_profile.compressed,
            partitions = effective_partitions,
            "applying stream profile to hot buffer"
        );
    }
    if let Err(err) = state.hot_buffer.ensure_topic(stream_id, effective_partitions).await {
        tracing::warn!(
            stream_id = %stream_id,
            error = %err,
            "hot buffer ensure_topic failed; stream created without backing topic"
        );
    }

    let row = load_stream_row(&state.db, stream_id)
        .await
        .map_err(|cause| db_error(&cause))?;

    let definition: StreamDefinition = row.into();
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
    .bind(
        payload
            .partitions
            .map(|p| p.clamp(1, 1024))
            .unwrap_or(existing.partitions),
    )
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
    .execute(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    // If a fresh Avro schema was provided, append it to history (next
    // version after the current max).
    if let (Some(schema), Some(fp)) = (new_avro.as_ref(), new_fingerprint.as_ref()) {
        let next_version: i32 = sqlx::query_scalar(
            "SELECT COALESCE(MAX(version), 0) + 1 FROM streaming_stream_schema_history WHERE stream_id = $1",
        )
        .bind(id)
        .fetch_one(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
        let _ = insert_schema_history_row(&state.db, id, next_version, schema, fp, &compat_mode, "operator")
            .await
            .map_err(|err| tracing::warn!(stream = %id, error = %err, "schema history append failed"));
    }

    let row = load_stream_row(&state.db, id)
        .await
        .map_err(|cause| db_error(&cause))?;

    let definition: StreamDefinition = row.into();
    emit_audit_event(
        &claims,
        "streaming.stream.updated",
        "streaming_stream",
        definition.id,
        serde_json::json!({ "name": definition.name }),
    );
    Ok(Json(definition))
}

pub async fn list_windows(
    axum::extract::State(state): axum::extract::State<AppState>,
) -> ServiceResult<ListResponse<WindowDefinition>> {
    let rows = sqlx::query_as::<_, WindowRow>(
        "SELECT id, name, description, status, window_type, duration_seconds, slide_seconds,
		        session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields,
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

    sqlx::query(
        "INSERT INTO streaming_windows (
		    id, name, description, status, window_type, duration_seconds, slide_seconds,
		    session_gap_seconds, allowed_lateness_seconds, aggregation_keys, measure_fields
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)",
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
    .execute(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let row = load_window_row(&state.db, window_id)
        .await
        .map_err(|cause| db_error(&cause))?;

    Ok(Json(row.into()))
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
    .execute(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let row = load_window_row(&state.db, id)
        .await
        .map_err(|cause| db_error(&cause))?;

    Ok(Json(row.into()))
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

        let sequence_no = sqlx::query_scalar::<_, i64>(
            r#"INSERT INTO streaming_events (id, stream_id, payload, event_time)
               VALUES ($1, $2, $3, $4)
               RETURNING sequence_no"#,
        )
        .bind(Uuid::now_v7())
        .bind(stream_id)
        .bind(SqlJson(event.payload.clone()))
        .bind(event_time)
        .fetch_one(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;

        // Mirror the accepted event into the hot buffer (Kafka/NATS). When
        // the publish fails (broker down, transient network error) we
        // dead-letter the event so operators can inspect & replay it. The
        // Postgres row stays in place for read paths that don't depend on
        // the hot buffer (audit, replay).
        let payload_bytes = serde_json::to_vec(&event.payload).unwrap_or_default();
        let key = event
            .payload
            .get("id")
            .and_then(|v| v.as_str())
            .map(str::to_owned);
        if let Err(err) = state
            .hot_buffer
            .publish(stream_id, key.as_deref(), &payload_bytes)
            .await
        {
            tracing::warn!(
                stream_id = %stream_id,
                sequence_no,
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

        accepted_events += 1;
        if first_sequence_no.is_none() {
            first_sequence_no = Some(sequence_no);
        }
        last_sequence_no = Some(sequence_no);
    }

    state
        .metrics
        .record_stream_rows_in(&stream.name, accepted_events as u64);

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

    let sequence_no = sqlx::query_scalar::<_, i64>(
        r#"INSERT INTO streaming_events (id, stream_id, payload, event_time)
           VALUES ($1, $2, $3, $4)
           RETURNING sequence_no"#,
    )
    .bind(Uuid::now_v7())
    .bind(dead_letter.stream_id)
    .bind(SqlJson(next_payload.clone()))
    .bind(next_event_time)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

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
///   1. Compute `cold_watermark = MAX(snapshot_at) FROM
///      streaming_cold_archives WHERE stream_id = $1`. Anything older
///      than that watermark is guaranteed to be available in cold.
///   2. Always query the live `streaming_events` table tagged `source =
///      "hot"` filtered by `from`/`to` (Postgres still keeps everything
///      until retention kicks in, so this overlaps cold by design).
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
    let from = params.from.unwrap_or_else(|| Utc::now() - chrono::Duration::hours(24));
    let to = params.to.unwrap_or_else(Utc::now);
    if from >= to {
        return Err(bad_request("`from` must be strictly before `to`"));
    }

    let cold_watermark: Option<DateTime<Utc>> = sqlx::query_scalar(
        "SELECT MAX(snapshot_at) FROM streaming_cold_archives WHERE stream_id = $1",
    )
    .bind(stream_id)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let mut merged: Vec<ReadStreamRow> = Vec::with_capacity(limit as usize);

    let hot_rows: Vec<(i64, DateTime<Utc>, SqlJson<Value>)> = sqlx::query_as(
        "SELECT sequence_no, event_time, payload
           FROM streaming_events
          WHERE stream_id = $1 AND event_time BETWEEN $2 AND $3
          ORDER BY event_time ASC
          LIMIT $4",
    )
    .bind(stream_id)
    .bind(from)
    .bind(to)
    .bind(limit)
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    for (seq, ts, payload) in hot_rows {
        merged.push(ReadStreamRow {
            sequence_no: Some(seq),
            event_time: ts,
            payload: payload.0,
            source: "hot",
            snapshot_id: None,
            parquet_path: None,
        });
    }

    if cold_watermark.map(|w| from < w).unwrap_or(false) {
        let cold_rows: Vec<(String, DateTime<Utc>, String, i64)> = sqlx::query_as(
            "SELECT snapshot_id, snapshot_at, parquet_path, row_count
               FROM streaming_cold_archives
              WHERE stream_id = $1 AND snapshot_at BETWEEN $2 AND $3
              ORDER BY snapshot_at ASC
              LIMIT $4",
        )
        .bind(stream_id)
        .bind(from)
        .bind(to)
        .bind(limit)
        .fetch_all(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
        for (snapshot_id, ts, path, row_count) in cold_rows {
            merged.push(ReadStreamRow {
                sequence_no: None,
                event_time: ts,
                payload: serde_json::json!({
                    "row_count": row_count,
                    "hint": "fetch parquet file at parquet_path for raw rows",
                }),
                source: "cold",
                snapshot_id: Some(snapshot_id),
                parquet_path: Some(path),
            });
        }
    }

    merged.sort_by_key(|r| r.event_time);
    merged.truncate(limit as usize);

    Ok(Json(ListResponse { data: merged }))
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
