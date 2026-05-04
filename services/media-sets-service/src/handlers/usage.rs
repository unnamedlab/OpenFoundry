//! `GET /media-sets/{rid}/usage` — compute-seconds + bytes meter for
//! the Usage UI tab and any external billing exporter.
//!
//! The endpoint reads from `media_set_access_pattern_invocations`
//! (the H5 ledger) directly so it never depends on an external
//! Prometheus scrape. Range defaults to the trailing 30 days; the
//! UI passes `since=` to drill in.

use axum::{
    Json,
    extract::{Path, Query, State},
};
use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

use crate::AppState;
use crate::domain::cedar::{action_view, check_media_set};
use crate::domain::error::MediaResult;
use crate::handlers::media_sets::{MediaErrorResponse, get_media_set_op};

#[derive(Debug, Clone, Deserialize)]
pub struct UsageQuery {
    /// ISO-8601. Defaults to `now() - 30 days`.
    pub since: Option<DateTime<Utc>>,
    /// ISO-8601. Defaults to `now()`.
    pub until: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct UsageBucketByKind {
    pub kind: String,
    pub invocations: i64,
    pub cache_hits: i64,
    pub compute_seconds: i64,
    pub input_bytes: i64,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct UsageDailyPoint {
    pub day: chrono::NaiveDate,
    pub kind: String,
    pub compute_seconds: i64,
    pub input_bytes: i64,
}

/// Combined response — one shot fills both the per-kind summary card
/// and the per-day stacked-bar chart the Usage tab renders.
#[derive(Debug, Clone, Serialize)]
pub struct UsageResponse {
    pub since: DateTime<Utc>,
    pub until: DateTime<Utc>,
    pub total_compute_seconds: i64,
    pub total_input_bytes: i64,
    pub by_kind: Vec<UsageBucketByKind>,
    pub by_day_kind: Vec<UsageDailyPoint>,
}

pub async fn get_usage_op(
    state: &AppState,
    media_set_rid: &str,
    since: DateTime<Utc>,
    until: DateTime<Utc>,
) -> MediaResult<UsageResponse> {
    let by_kind: Vec<UsageBucketByKind> = sqlx::query_as(
        r#"SELECT kind,
                  COUNT(*)::bigint                                            AS invocations,
                  COUNT(*) FILTER (WHERE cache_hit)::bigint                   AS cache_hits,
                  COALESCE(SUM(compute_seconds), 0)::bigint                   AS compute_seconds,
                  COALESCE(SUM(input_bytes), 0)::bigint                       AS input_bytes
             FROM media_set_access_pattern_invocations
            WHERE media_set_rid = $1
              AND invoked_at >= $2
              AND invoked_at <  $3
         GROUP BY kind
         ORDER BY compute_seconds DESC, kind ASC"#,
    )
    .bind(media_set_rid)
    .bind(since)
    .bind(until)
    .fetch_all(state.db.reader())
    .await?;

    let by_day_kind: Vec<UsageDailyPoint> = sqlx::query_as(
        r#"SELECT (date_trunc('day', invoked_at))::date          AS day,
                  kind,
                  COALESCE(SUM(compute_seconds), 0)::bigint      AS compute_seconds,
                  COALESCE(SUM(input_bytes), 0)::bigint          AS input_bytes
             FROM media_set_access_pattern_invocations
            WHERE media_set_rid = $1
              AND invoked_at >= $2
              AND invoked_at <  $3
         GROUP BY day, kind
         ORDER BY day ASC, kind ASC"#,
    )
    .bind(media_set_rid)
    .bind(since)
    .bind(until)
    .fetch_all(state.db.reader())
    .await?;

    let total_compute_seconds = by_kind.iter().map(|b| b.compute_seconds).sum();
    let total_input_bytes = by_kind.iter().map(|b| b.input_bytes).sum();

    Ok(UsageResponse {
        since,
        until,
        total_compute_seconds,
        total_input_bytes,
        by_kind,
        by_day_kind,
    })
}

pub async fn get_usage(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
    Query(q): Query<UsageQuery>,
) -> Result<Json<UsageResponse>, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_view(), &set).await?;
    let until = q.until.unwrap_or_else(Utc::now);
    let since = q.since.unwrap_or_else(|| until - Duration::days(30));
    let resp = get_usage_op(&state, &rid, since, until).await?;
    Ok(Json(resp))
}
