//! Live-log HTTP surface (Foundry Builds.md § Live logs).
//!
//! Three endpoints sit on top of [`crate::domain::logs`]:
//!
//!   * `GET /v1/jobs/{rid}/logs` — paginated REST history.
//!   * `GET /v1/jobs/{rid}/logs/stream` — Server-Sent Events.
//!   * `GET /v1/jobs/{rid}/logs/ws` — WebSocket (same payload).
//!
//! The SSE stream waits a deliberate 10 seconds before emitting buffered
//! entries to mirror Foundry's documented "ten-second delay may occur
//! before live logs are visible". The wait is communicated via
//! `event: heartbeat` SSE events so the UI can render an
//! "Initializing…" badge instead of an indeterminate spinner.

use std::convert::Infallible;
use std::time::Duration;

use auth_middleware::layer::AuthUser;
use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::response::sse::{Event, KeepAlive, Sse};
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use futures::stream::{self, StreamExt};
use serde::{Deserialize, Serialize};
use serde_json::json;
use tokio_stream::wrappers::BroadcastStream;

use crate::AppState;
use crate::domain::logs::{LogEntry, LogLevel};
use crate::domain::metrics;

/// Foundry doc: "Once enabled, a ten-second delay may occur before
/// the live logs are visible in the interface."
pub const SSE_INITIAL_DELAY_SECS: u64 = 10;

#[derive(Debug, Deserialize, Default)]
pub struct LogsQuery {
    #[serde(default)]
    pub from_sequence: Option<i64>,
    #[serde(default)]
    pub since: Option<DateTime<Utc>>,
    #[serde(default)]
    pub until: Option<DateTime<Utc>>,
    /// Comma-separated list. Empty / missing = all levels.
    #[serde(default)]
    pub levels: Option<String>,
    #[serde(default)]
    pub limit: Option<i64>,
    /// SSE only — when false, the stream closes after the catch-up
    /// phase. Default true.
    #[serde(default)]
    pub follow: Option<bool>,
}

impl LogsQuery {
    fn parsed_levels(&self) -> Vec<String> {
        self.levels
            .as_deref()
            .map(|csv| {
                csv.split(',')
                    .map(|s| s.trim())
                    .filter_map(|s| s.parse::<LogLevel>().ok())
                    .map(|l| l.as_str().to_string())
                    .collect()
            })
            .unwrap_or_default()
    }
}

#[derive(Debug, Serialize)]
struct LogRowDto {
    sequence: i64,
    ts: DateTime<Utc>,
    level: String,
    message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<serde_json::Value>,
}

async fn resolve_job(
    state: &AppState,
    rid: &str,
) -> Result<uuid::Uuid, StatusCode> {
    sqlx::query_scalar::<_, uuid::Uuid>("SELECT id FROM jobs WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(|_| StatusCode::INTERNAL_SERVER_ERROR)?
        .ok_or(StatusCode::NOT_FOUND)
}

// ---------------------------------------------------------------------------
// REST history
// ---------------------------------------------------------------------------

pub async fn list_logs(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<LogsQuery>,
) -> impl IntoResponse {
    let job_id = match resolve_job(&state, &rid).await {
        Ok(j) => j,
        Err(s) => return s.into_response(),
    };
    let limit = params.limit.unwrap_or(500).clamp(1, 5000);
    let from_seq = params.from_sequence.unwrap_or(0);
    let levels = params.parsed_levels();

    let rows: Vec<(i64, DateTime<Utc>, String, String, Option<serde_json::Value>)> =
        sqlx::query_as(
            r#"SELECT sequence, ts, level, message, params
                 FROM job_logs
                WHERE job_id = $1
                  AND sequence >= $2
                  AND ($3::timestamptz IS NULL OR ts >= $3)
                  AND ($4::timestamptz IS NULL OR ts < $4)
                  AND (
                      cardinality($5::text[]) = 0
                      OR level = ANY($5)
                  )
                ORDER BY sequence ASC
                LIMIT $6"#,
        )
        .bind(job_id)
        .bind(from_seq)
        .bind(params.since)
        .bind(params.until)
        .bind(&levels)
        .bind(limit)
        .fetch_all(&state.db)
        .await
        .unwrap_or_default();

    let dto: Vec<LogRowDto> = rows
        .into_iter()
        .map(|(sequence, ts, level, message, params)| LogRowDto {
            sequence,
            ts,
            level,
            message,
            params,
        })
        .collect();

    let next_from = dto.last().map(|r| r.sequence + 1);
    Json(json!({
        "rid": rid,
        "data": dto,
        "next_from_sequence": next_from,
    }))
    .into_response()
}

// ---------------------------------------------------------------------------
// SSE — /logs/stream
// ---------------------------------------------------------------------------

pub async fn stream_logs(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<LogsQuery>,
) -> impl IntoResponse {
    let ports = match state.lifecycle_ports.clone() {
        Some(p) => p,
        None => {
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                "live logs not configured",
            )
                .into_response();
        }
    };
    let job_id = match resolve_job(&state, &rid).await {
        Ok(j) => j,
        Err(s) => return s.into_response(),
    };
    let from_seq = params.from_sequence.unwrap_or(0);
    let follow = params.follow.unwrap_or(true);
    let levels = params.parsed_levels();
    let levels_filter = if levels.is_empty() { None } else { Some(levels) };

    let pool = state.db.clone();
    let job_rid = rid.clone();

    // Audit subscription start (R11 in the prompt). Doubles as the
    // "subscribed" event in the audit feed.
    tracing::info!(
        target: "audit",
        action = "job.logs.subscribed",
        job_rid = job_rid.as_str(),
        from_sequence = from_seq,
        follow = follow,
        "live-log subscriber connected"
    );

    let history_rows: Vec<(i64, DateTime<Utc>, String, String, Option<serde_json::Value>)> =
        sqlx::query_as(
            r#"SELECT sequence, ts, level, message, params
                 FROM job_logs
                WHERE job_id = $1
                  AND sequence >= $2
                  AND (
                      $3::text[] IS NULL
                      OR cardinality($3::text[]) = 0
                      OR level = ANY($3)
                  )
                ORDER BY sequence ASC"#,
        )
        .bind(job_id)
        .bind(from_seq)
        .bind(&levels_filter)
        .fetch_all(&pool)
        .await
        .unwrap_or_default();

    let live_rx = if follow {
        Some(ports.broadcaster.subscribe(&job_rid).await)
    } else {
        None
    };

    // Build the combined stream:
    //   1. Initial "heartbeat" event announcing the configured delay.
    //   2. SSE_INITIAL_DELAY_SECS one-second heartbeats so the UI can
    //      render a counting "Initializing…" badge.
    //   3. Catch-up history.
    //   4. (optional) Live tail from the broadcast channel.
    let levels_for_filter = levels_filter.clone();
    let heartbeat_stream =
        async_stream::stream! {
            yield Ok::<Event, Infallible>(
                Event::default()
                    .event("heartbeat")
                    .json_data(json!({
                        "phase": "initializing",
                        "delay_remaining_seconds": SSE_INITIAL_DELAY_SECS,
                        "message": "Live logs are streamed in real-time. Time range filters do not apply.",
                    }))
                    .unwrap_or_else(|_| Event::default().event("heartbeat")),
            );
            for remaining in (1..=SSE_INITIAL_DELAY_SECS).rev() {
                tokio::time::sleep(Duration::from_secs(1)).await;
                yield Ok(Event::default()
                    .event("heartbeat")
                    .json_data(json!({
                        "phase": "initializing",
                        "delay_remaining_seconds": remaining - 1,
                    }))
                    .unwrap_or_else(|_| Event::default().event("heartbeat")));
            }
        };

    let history_stream = stream::iter(history_rows.into_iter().map(move |row| {
        let dto = LogRowDto {
            sequence: row.0,
            ts: row.1,
            level: row.2,
            message: row.3,
            params: row.4,
        };
        Ok::<Event, Infallible>(
            Event::default()
                .event("log")
                .json_data(&dto)
                .unwrap_or_else(|_| Event::default().event("log")),
        )
    }));

    let live_stream = match live_rx {
        Some(rx) => {
            let levels = levels_for_filter.clone();
            let job_rid = job_rid.clone();
            BroadcastStream::new(rx)
                .filter_map(move |entry| {
                    let levels = levels.clone();
                    let job_rid = job_rid.clone();
                    async move {
                        let entry = entry.ok()?;
                        if entry.job_rid != job_rid {
                            return None;
                        }
                        if let Some(filter) = &levels {
                            if !filter.iter().any(|l| l == entry.level.as_str()) {
                                return None;
                            }
                        }
                        let dto = LogRowDto {
                            sequence: entry.sequence,
                            ts: entry.ts,
                            level: entry.level.as_str().to_string(),
                            message: entry.message,
                            params: entry.params,
                        };
                        Some(Ok::<Event, Infallible>(
                            Event::default()
                                .event("log")
                                .json_data(&dto)
                                .unwrap_or_else(|_| Event::default().event("log")),
                        ))
                    }
                })
                .boxed()
        }
        None => stream::empty().boxed(),
    };

    let combined = heartbeat_stream
        .chain(history_stream)
        .chain(live_stream);

    Sse::new(combined)
        .keep_alive(KeepAlive::new().interval(Duration::from_secs(15)))
        .into_response()
}

// ---------------------------------------------------------------------------
// WebSocket — /logs/ws
// ---------------------------------------------------------------------------

pub async fn ws_logs(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<LogsQuery>,
    ws: WebSocketUpgrade,
) -> impl IntoResponse {
    let ports = match state.lifecycle_ports.clone() {
        Some(p) => p,
        None => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let job_rid = rid.clone();
    let from_seq = params.from_sequence.unwrap_or(0);
    let levels = params.parsed_levels();
    let levels_filter = if levels.is_empty() { None } else { Some(levels) };
    let pool = state.db.clone();

    ws.on_upgrade(move |socket| async move {
        if let Err(err) =
            run_ws(socket, pool, ports, job_rid, from_seq, levels_filter).await
        {
            tracing::warn!(error = %err, "ws session ended");
        }
    })
    .into_response()
}

async fn run_ws(
    mut socket: WebSocket,
    pool: sqlx::PgPool,
    ports: crate::handlers::builds_v1::BuildLifecyclePorts,
    job_rid: String,
    from_seq: i64,
    levels_filter: Option<Vec<String>>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let job_id: Option<(uuid::Uuid,)> =
        sqlx::query_as("SELECT id FROM jobs WHERE rid = $1")
            .bind(&job_rid)
            .fetch_optional(&pool)
            .await?;
    let Some((job_id,)) = job_id else {
        let _ = socket.send(Message::Close(None)).await;
        return Ok(());
    };

    // Catch-up.
    let history: Vec<(i64, DateTime<Utc>, String, String, Option<serde_json::Value>)> =
        sqlx::query_as(
            r#"SELECT sequence, ts, level, message, params
                 FROM job_logs
                WHERE job_id = $1
                  AND sequence >= $2
                ORDER BY sequence ASC"#,
        )
        .bind(job_id)
        .bind(from_seq)
        .fetch_all(&pool)
        .await?;
    for (sequence, ts, level, message, params) in history {
        if let Some(filter) = &levels_filter {
            if !filter.iter().any(|l| l == &level) {
                continue;
            }
        }
        let dto = LogRowDto { sequence, ts, level, message, params };
        let frame = serde_json::to_string(&dto)?;
        if socket.send(Message::Text(frame.into())).await.is_err() {
            return Ok(());
        }
    }

    // Live.
    let mut rx = ports.broadcaster.subscribe(&job_rid).await;
    while let Ok(entry) = rx.recv().await {
        if entry.job_rid != job_rid {
            continue;
        }
        if let Some(filter) = &levels_filter {
            if !filter.iter().any(|l| l == entry.level.as_str()) {
                continue;
            }
        }
        let dto = LogRowDto {
            sequence: entry.sequence,
            ts: entry.ts,
            level: entry.level.as_str().to_string(),
            message: entry.message,
            params: entry.params,
        };
        let frame = serde_json::to_string(&dto)?;
        if socket.send(Message::Text(frame.into())).await.is_err() {
            break;
        }
    }
    metrics::set_live_log_subscribers_for(&job_rid, 0);
    tracing::info!(
        target: "audit",
        action = "job.logs.unsubscribed",
        job_rid = job_rid.as_str(),
        "live-log subscriber disconnected"
    );
    Ok(())
}

// ---------------------------------------------------------------------------
// POST /v1/jobs/{rid}/logs:emit  (internal — runner pushes here)
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct EmitLogRequest {
    pub level: LogLevel,
    pub message: String,
    #[serde(default)]
    pub params: Option<serde_json::Value>,
}

pub async fn emit_log(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Json(body): Json<EmitLogRequest>,
) -> impl IntoResponse {
    let ports = match state.lifecycle_ports.clone() {
        Some(p) => p,
        None => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let entry = LogEntry {
        sequence: 0,
        job_rid: rid.clone(),
        ts: Utc::now(),
        level: body.level,
        message: body.message,
        params: body.params,
    };
    match ports.log_sink.emit(entry).await {
        Ok(seq) => Json(json!({"rid": rid, "sequence": seq})).into_response(),
        Err(err) => {
            tracing::warn!(error = %err, "log emit failed");
            (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response()
        }
    }
}

