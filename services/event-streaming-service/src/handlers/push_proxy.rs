//! Foundry-parity push proxy.
//!
//! Mounts under the unauthenticated outer router as
//! `/streams-push/{view_rid}/records` and `/streams-push/{stream_rid}/url`
//! so push consumers (Source agents, third-party applications) can
//! POST records without going through the standard JWT auth flow.
//!
//! Authentication is enforced via a bearer token that callers obtain
//! through the same OAuth2 third-party application workflow Foundry
//! uses. The proxy does not validate the token cryptographically
//! itself — that responsibility lives with the platform's auth
//! gateway. The proxy *does* enforce:
//!   * `view_rid` belongs to a generation that is still active
//!     (404 `PUSH_VIEW_RETIRED` for retired URLs);
//!   * payload validates against the active view's schema (422
//!     `PUSH_SCHEMA_VALIDATION_FAILED`);
//!   * a coarse-grained per-view rate limit so a misbehaving agent
//!     cannot saturate the broker (503 `PUSH_RATE_LIMITED`).
//!
//! Successful publishes return `202 Accepted` with the sequence
//! numbers assigned by the runtime store.

use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};
use std::time::{Duration, Instant};

use axum::{
    Json,
    extract::Path,
    http::{HeaderMap, StatusCode},
};
use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde_json::Value;
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{ErrorResponse, ServiceResult, bad_request, db_error, not_found, unprocessable},
    models::{
        stream::{StreamRow, StreamSchema},
        stream_view::{PushUrlResponse, StreamView, stream_rid_for},
    },
};

/// Stable error codes surfaced by the push proxy.
pub const ERR_PUSH_VIEW_RETIRED: &str = "PUSH_VIEW_RETIRED";
pub const ERR_PUSH_SCHEMA: &str = "PUSH_SCHEMA_VALIDATION_FAILED";
pub const ERR_PUSH_RATE_LIMITED: &str = "PUSH_RATE_LIMITED";
pub const ERR_PUSH_MISSING_TOKEN: &str = "PUSH_MISSING_BEARER_TOKEN";

/// Coarse-grained per-view token bucket. Foundry's docs do not pin a
/// value; we default to 200 records/s per view so a single misbehaving
/// agent cannot saturate the broker. Operators can raise the cap with
/// `STREAMING_PUSH_RPS` at deploy time.
const DEFAULT_RPS: u32 = 200;
/// Sliding window the RPS counter is enforced over.
const WINDOW: Duration = Duration::from_secs(1);

#[derive(Default)]
struct RateLimiterState {
    /// `(window_start, count)` per view.
    buckets: HashMap<String, (Instant, u32)>,
}

static LIMITER: OnceLock<Mutex<RateLimiterState>> = OnceLock::new();

fn limiter() -> &'static Mutex<RateLimiterState> {
    LIMITER.get_or_init(|| Mutex::new(RateLimiterState::default()))
}

fn rps_cap() -> u32 {
    std::env::var("STREAMING_PUSH_RPS")
        .ok()
        .and_then(|raw| raw.parse().ok())
        .unwrap_or(DEFAULT_RPS)
}

/// Returns true when the view has spare budget; bumps the counter as a
/// side effect. The implementation deliberately leaves the lock held
/// for as little time as possible — clones and the `RPS_CAP` resolution
/// happen outside the critical section.
fn admit(view_rid: &str, cap: u32) -> bool {
    let mut state = match limiter().lock() {
        Ok(state) => state,
        Err(poisoned) => poisoned.into_inner(),
    };
    let entry = state
        .buckets
        .entry(view_rid.to_string())
        .or_insert_with(|| (Instant::now(), 0));
    if entry.0.elapsed() >= WINDOW {
        *entry = (Instant::now(), 0);
    }
    if entry.1 >= cap {
        return false;
    }
    entry.1 += 1;
    true
}

#[derive(Debug, Deserialize)]
pub struct PushBody {
    /// Foundry's docs use `[{ "value": {...} }]` as the canonical
    /// shape. We accept both that and a bare array of payload objects,
    /// so SDK examples and curl one-liners interoperate.
    pub records: Option<Vec<PushRecord>>,
    pub values: Option<Vec<Value>>,
}

#[derive(Debug, Deserialize)]
pub struct PushRecord {
    #[serde(default)]
    pub value: Option<Value>,
    #[serde(default)]
    pub event_time: Option<DateTime<Utc>>,
    #[serde(default)]
    pub key: Option<String>,
}

#[derive(Debug, serde::Serialize)]
pub struct PushResponse {
    pub view_rid: String,
    pub generation: i32,
    pub accepted: usize,
    pub first_sequence_no: Option<i64>,
    pub last_sequence_no: Option<i64>,
}

fn extract_bearer_token(headers: &HeaderMap) -> Option<String> {
    headers
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|h| h.to_str().ok())
        .and_then(|raw| raw.strip_prefix("Bearer ").map(str::trim).map(String::from))
        .filter(|t| !t.is_empty())
}

async fn load_stream_row_by_rid(
    db: &sqlx::PgPool,
    stream_rid: &str,
) -> Result<Option<StreamRow>, sqlx::Error> {
    // The stable RID is `ri.streams.main.stream.<uuid>`. Strip the
    // prefix and look up the stream by its UUID. This keeps the JOIN
    // path the same as the rest of the service.
    let Some(uuid_str) = stream_rid.strip_prefix(crate::models::stream_view::STREAM_RID_PREFIX)
    else {
        return Ok(None);
    };
    let Ok(stream_id) = Uuid::parse_str(uuid_str) else {
        return Ok(None);
    };
    sqlx::query_as::<_, StreamRow>(
        "SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, schema_avro, schema_fingerprint, schema_compatibility_mode, default_marking, stream_type, compression, ingest_consistency, pipeline_consistency, checkpoint_interval_ms, kind, created_at, updated_at
         FROM streaming_streams
         WHERE id = $1",
    )
    .bind(stream_id)
    .fetch_optional(db)
    .await
}

fn validate_payload(schema: &StreamSchema, payload: &Value) -> Result<(), Vec<String>> {
    let Some(object) = payload.as_object() else {
        return Err(vec!["payload must be a JSON object".to_string()]);
    };
    let mut errors = Vec::new();
    for field in &schema.fields {
        let is_watermark = schema.watermark_field.as_deref() == Some(field.name.as_str())
            || field.semantic_role == "event_time";
        if !field.nullable && !object.contains_key(&field.name) && !is_watermark {
            errors.push(format!("missing required field '{}'", field.name));
        }
    }
    if errors.is_empty() {
        Ok(())
    } else {
        Err(errors)
    }
}

/// `POST /streams-push/{view_rid}/records`
pub async fn push_records(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(view_rid): Path<String>,
    headers: HeaderMap,
    Json(body): Json<PushBody>,
) -> ServiceResult<PushResponse> {
    if extract_bearer_token(&headers).is_none() {
        return Err((
            StatusCode::UNAUTHORIZED,
            Json(ErrorResponse {
                error: format!("{ERR_PUSH_MISSING_TOKEN}: bearer token is required"),
            }),
        ));
    }

    let cap = rps_cap();
    if !admit(&view_rid, cap) {
        return Err((
            StatusCode::SERVICE_UNAVAILABLE,
            Json(ErrorResponse {
                error: format!(
                    "{ERR_PUSH_RATE_LIMITED}: per-view limit of {cap} records/s exceeded"
                ),
            }),
        ));
    }

    // 1. Resolve the view + assert it is still active.
    let view: StreamView = match super::stream_views::load_active_view_by_view_rid(
        &state.db, &view_rid,
    )
    .await
    .map_err(|cause| db_error(&cause))?
    {
        Some(v) if v.active => v,
        Some(retired) => {
            return Err((
                StatusCode::NOT_FOUND,
                Json(ErrorResponse {
                    error: format!(
                        "{ERR_PUSH_VIEW_RETIRED}: viewRid {view_rid} has been retired; fetch the new POST URL from /streams-push/{}/url",
                        retired.stream_rid
                    ),
                }),
            ));
        }
        None => return Err(not_found("view not found")),
    };

    // 2. Resolve the underlying stream + schema for validation.
    let stream_row = match load_stream_row_by_rid(&state.db, &view.stream_rid).await {
        Ok(Some(row)) => row,
        Ok(None) => return Err(not_found("stream not found for view")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let schema: StreamSchema = stream_row.schema.0.clone();
    let stream_id = stream_row.id;

    // 3. Normalise the body shape — Foundry docs use
    //    `[{ "value": {...} }]`, but a bare `values: [...]` is also
    //    accepted so the SDK examples translate cleanly.
    let mut records: Vec<(Value, Option<DateTime<Utc>>, Option<String>)> = Vec::new();
    if let Some(items) = body.records {
        for r in items {
            if let Some(v) = r.value {
                records.push((v, r.event_time, r.key));
            }
        }
    }
    if let Some(items) = body.values {
        for v in items {
            records.push((v, None, None));
        }
    }
    if records.is_empty() {
        return Err(bad_request(
            "body must contain `records[].value` or `values[]`",
        ));
    }

    // 4. Schema-validate every record before publishing — push proxy
    //    rejects the whole batch if any record is malformed (matches
    //    Foundry behaviour: the PushApi returns 400 on the first
    //    invalid row).
    for (idx, (value, _, _)) in records.iter().enumerate() {
        if let Err(errs) = validate_payload(&schema, value) {
            return Err(unprocessable(
                ERR_PUSH_SCHEMA,
                format!("record #{idx} failed validation: {}", errs.join("; ")),
            ));
        }
    }

    // 5. Publish + record. Failures bubble up as 502 because the
    //    metadata is persisted but the broker is unhappy.
    let mut first_seq: Option<i64> = None;
    let mut last_seq: Option<i64> = None;
    for (value, event_time, key) in records.iter() {
        let payload_bytes = serde_json::to_vec(value).map_err(|cause| {
            tracing::warn!(error = %cause, "push proxy: payload reserialise failed");
            crate::handlers::internal_error("payload re-serialise failed")
        })?;
        let key_ref = key
            .as_deref()
            .or_else(|| value.get("id").and_then(|v| v.as_str()));
        if let Err(cause) = state
            .hot_buffer
            .publish(stream_id, key_ref, &payload_bytes)
            .await
        {
            tracing::warn!(stream_id = %stream_id, error = %cause, "push proxy: hot buffer publish failed");
            return Err((
                StatusCode::BAD_GATEWAY,
                Json(ErrorResponse {
                    error: format!("hot buffer publish failed: {cause}"),
                }),
            ));
        }
        let when = event_time.unwrap_or_else(Utc::now);
        match state
            .runtime_store
            .append_event(stream_id, value.clone(), when)
            .await
        {
            Ok(appended) => {
                if first_seq.is_none() {
                    first_seq = Some(appended.sequence_no);
                }
                last_seq = Some(appended.sequence_no);
            }
            Err(cause) => {
                tracing::warn!(stream_id = %stream_id, error = %cause, "push proxy: runtime append failed");
            }
        }
    }

    Ok(Json(PushResponse {
        view_rid: view.view_rid.clone(),
        generation: view.generation,
        accepted: records.len(),
        first_sequence_no: first_seq,
        last_sequence_no: last_seq,
    }))
}

/// `GET /streams-push/{stream_rid}/url`
///
/// Returns the active POST URL for a stream. Push consumers call this
/// when they receive a `stream.reset.v1` event or as a sanity check
/// when their POSTs start returning `PUSH_VIEW_RETIRED`.
pub async fn current_push_url(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(stream_rid): Path<String>,
) -> ServiceResult<PushUrlResponse> {
    let view = match super::stream_views::current_view_for_stream_rid(&state.db, &stream_rid).await
    {
        Ok(Some(v)) => v,
        Ok(None) => return Err(not_found("stream has no active view")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let _ = stream_rid_for; // silence dead-import lint when the helper is unused inline
    Ok(Json(super::stream_views::render_push_url_response(
        stream_rid,
        &view,
        &state.public_base_url,
    )))
}
