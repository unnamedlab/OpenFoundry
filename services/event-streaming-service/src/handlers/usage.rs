//! Bloque P6 — streaming compute usage handlers.
//!
//! Exposes:
//!   * `GET /v1/streams/{id}/usage?from=&to=&group=hour|day`
//!   * `GET /v1/topologies/{id}/usage?from=&to=&group=hour|day`
//!
//! Both queries roll up the `stream_compute_usage` table by hour or
//! day and surface a small JSON shape the Usage tab and the
//! `monitoring-rules-service` evaluator can consume.

use axum::{
    Json,
    extract::{Path, Query, State},
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

use crate::AppState;
use crate::handlers::{ServiceResult, bad_request, db_error, not_found};
use crate::models::stream_view::stream_rid_for;

#[derive(Debug, Default, Deserialize)]
pub struct UsageQuery {
    pub from: Option<DateTime<Utc>>,
    pub to: Option<DateTime<Utc>>,
    /// `hour` (default) or `day`.
    pub group: Option<String>,
}

#[derive(Debug, Clone, Serialize, sqlx::FromRow)]
pub struct UsageBucket {
    pub bucket_start: DateTime<Utc>,
    pub compute_seconds: f64,
    pub records_processed: i64,
}

#[derive(Debug, Serialize)]
pub struct UsageResponse {
    pub from: DateTime<Utc>,
    pub to: DateTime<Utc>,
    pub group: &'static str,
    pub buckets: Vec<UsageBucket>,
    pub total_compute_seconds: f64,
    pub total_records_processed: i64,
}

fn parse_group(raw: Option<&str>) -> Result<&'static str, String> {
    match raw.unwrap_or("hour") {
        "hour" => Ok("hour"),
        "day" => Ok("day"),
        other => Err(format!("group must be hour|day (got {other})")),
    }
}

async fn rollup(
    db: &PgPool,
    selector_column: &str,
    selector_value: &str,
    from: DateTime<Utc>,
    to: DateTime<Utc>,
    group: &'static str,
) -> Result<Vec<UsageBucket>, sqlx::Error> {
    let truncate = if group == "day" { "day" } else { "hour" };
    let sql = format!(
        "SELECT date_trunc($1, ts) AS bucket_start,
                COALESCE(SUM(compute_seconds), 0)::float8 AS compute_seconds,
                COALESCE(SUM(records_processed), 0)::bigint AS records_processed
           FROM stream_compute_usage
          WHERE {selector_column} = $2
            AND ts >= $3
            AND ts <  $4
          GROUP BY 1
          ORDER BY 1 ASC",
    );
    sqlx::query_as::<_, UsageBucket>(&sql)
        .bind(truncate)
        .bind(selector_value)
        .bind(from)
        .bind(to)
        .fetch_all(db)
        .await
}

fn defaults(query: &UsageQuery) -> (DateTime<Utc>, DateTime<Utc>) {
    let to = query.to.unwrap_or_else(Utc::now);
    let from = query
        .from
        .unwrap_or_else(|| to - chrono::Duration::days(7));
    (from, to)
}

/// `GET /streams/{id}/usage`
pub async fn get_stream_usage(
    State(state): State<AppState>,
    Path(stream_id): Path<Uuid>,
    Query(query): Query<UsageQuery>,
) -> ServiceResult<UsageResponse> {
    // Confirm the stream exists for a sane 404.
    let exists: Option<(Uuid,)> =
        sqlx::query_as("SELECT id FROM streaming_streams WHERE id = $1")
            .bind(stream_id)
            .fetch_optional(&state.db)
            .await
            .map_err(|c| db_error(&c))?;
    if exists.is_none() {
        return Err(not_found("stream not found"));
    }

    let group = parse_group(query.group.as_deref()).map_err(bad_request)?;
    let (from, to) = defaults(&query);
    if from >= to {
        return Err(bad_request("from must be strictly before to"));
    }
    let stream_rid = stream_rid_for(stream_id);
    let buckets = rollup(&state.db, "stream_rid", &stream_rid, from, to, group)
        .await
        .map_err(|c| db_error(&c))?;
    let total_compute_seconds = buckets.iter().map(|b| b.compute_seconds).sum();
    let total_records_processed = buckets.iter().map(|b| b.records_processed).sum();
    Ok(Json(UsageResponse {
        from,
        to,
        group,
        buckets,
        total_compute_seconds,
        total_records_processed,
    }))
}

/// `GET /topologies/{id}/usage`
pub async fn get_topology_usage(
    State(state): State<AppState>,
    Path(topology_id): Path<Uuid>,
    Query(query): Query<UsageQuery>,
) -> ServiceResult<UsageResponse> {
    let exists: Option<(Uuid,)> =
        sqlx::query_as("SELECT id FROM streaming_topologies WHERE id = $1")
            .bind(topology_id)
            .fetch_optional(&state.db)
            .await
            .map_err(|c| db_error(&c))?;
    if exists.is_none() {
        return Err(not_found("topology not found"));
    }

    let group = parse_group(query.group.as_deref()).map_err(bad_request)?;
    let (from, to) = defaults(&query);
    if from >= to {
        return Err(bad_request("from must be strictly before to"));
    }
    let topology_rid = format!("ri.streams.main.topology.{topology_id}");
    let buckets = rollup(&state.db, "topology_rid", &topology_rid, from, to, group)
        .await
        .map_err(|c| db_error(&c))?;
    let total_compute_seconds = buckets.iter().map(|b| b.compute_seconds).sum();
    let total_records_processed = buckets.iter().map(|b| b.records_processed).sum();
    Ok(Json(UsageResponse {
        from,
        to,
        group,
        buckets,
        total_compute_seconds,
        total_records_processed,
    }))
}

/// Pure helper used by the engine on every checkpoint commit. Persists
/// a row in `stream_compute_usage`. The duration is `wall_time *
/// task_slots`; the engine multiplies them before calling.
pub async fn record_checkpoint_usage(
    db: &PgPool,
    stream_rid: &str,
    topology_rid: Option<&str>,
    compute_seconds: f64,
    records_processed: i64,
    partition: i32,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        "INSERT INTO stream_compute_usage
            (id, ts, stream_rid, topology_rid, compute_seconds, records_processed, partition)
         VALUES ($1, now(), $2, $3, $4, $5, $6)",
    )
    .bind(Uuid::now_v7())
    .bind(stream_rid)
    .bind(topology_rid)
    .bind(compute_seconds.max(0.0))
    .bind(records_processed.max(0))
    .bind(partition.max(0))
    .execute(db)
    .await?;
    Ok(())
}

/// Stable error code surfaced by the cost factor parser.
pub const ERR_USAGE_INVALID_GROUP: &str = "STREAM_USAGE_INVALID_GROUP";

/// Parse the optional `STREAMING_COMPUTE_SECONDS_TO_COST_FACTOR` env
/// override. Returns the factor (USD per compute-second). Defaults to
/// 0.0001 — well below realistic provider pricing — so a stale env
/// var doesn't cause silent overcharging.
pub fn cost_factor() -> f64 {
    std::env::var("STREAMING_COMPUTE_SECONDS_TO_COST_FACTOR")
        .ok()
        .and_then(|raw| raw.parse::<f64>().ok())
        .map(|v| v.clamp(0.0, 1.0))
        .unwrap_or(0.0001)
}

/// Best-effort cost projection helper used by the Usage tab.
pub fn project_cost(seconds: f64) -> f64 {
    seconds * cost_factor()
}

/// Re-exported for tests.
pub fn group_of(raw: Option<&str>) -> Result<&'static str, String> {
    parse_group(raw)
}

/// Convenience: emit an opaque JSON value summarising both rollups
/// for a stream. Used by the GET /usage endpoint when a caller wants
/// hour + day in a single response.
pub fn summarise_buckets(buckets: &[UsageBucket]) -> Value {
    let total = buckets.iter().map(|b| b.compute_seconds).sum::<f64>();
    serde_json::json!({
        "buckets": buckets,
        "total_compute_seconds": total,
        "total_records_processed":
            buckets.iter().map(|b| b.records_processed).sum::<i64>(),
    })
}
