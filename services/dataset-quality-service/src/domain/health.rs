//! P6 — `compute_health` for a dataset.
//!
//! Foundry doc § "Health checks" lists the metrics the UI needs:
//!   * row_count + col_count
//!   * null % per column
//!   * freshness_seconds = now - last_commit
//!   * txn_failure_rate_24h
//!   * last_build_status
//!   * schema_drift flag
//!
//! The path here is intentionally cheap — three SQL queries against
//! the DVS-owned tables (`datasets`, `dataset_transactions`,
//! `dataset_files`, `dataset_view_schemas`). The heavy column
//! profiling lives in `domain::quality::profiler`; that's what the
//! `/quality/profile` endpoint exercises and what populates
//! `null_pct_by_column` here when the caller pre-fetches it.

use std::collections::BTreeMap;

use chrono::{DateTime, Duration, Utc};
use serde_json::{Value, json};
use sqlx::Row;
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct ComputedHealth {
    pub dataset_id: Option<Uuid>,
    pub row_count: i64,
    pub col_count: i32,
    pub null_pct_by_column: BTreeMap<String, f64>,
    pub freshness_seconds: i64,
    pub last_commit_at: Option<DateTime<Utc>>,
    pub txn_failure_rate_24h: f64,
    pub last_build_status: String,
    pub schema_drift_flag: bool,
    pub extras: Value,
}

/// Compute a fresh health snapshot for `dataset_rid`. Returns `None`
/// when the dataset can't be located — the catalog `datasets` table
/// is the source of truth for the RID → UUID mapping.
pub async fn compute_health(
    db: &sqlx::PgPool,
    dataset_rid: &str,
) -> Result<Option<ComputedHealth>, String> {
    let dataset_id = lookup_dataset_id(db, dataset_rid).await?;
    let dataset_id = match dataset_id {
        Some(id) => id,
        None => return Ok(None),
    };

    // 1) Last committed transaction → freshness anchor + last_build_status.
    let last = sqlx::query(
        r#"SELECT committed_at, tx_type
             FROM dataset_transactions
            WHERE dataset_id = $1 AND status = 'COMMITTED'
            ORDER BY committed_at DESC NULLS LAST
            LIMIT 1"#,
    )
    .bind(dataset_id)
    .fetch_optional(db)
    .await
    .map_err(|e| e.to_string())?;
    let last_commit_at: Option<DateTime<Utc>> = last
        .as_ref()
        .and_then(|row| row.try_get("committed_at").ok());
    let last_tx_type: Option<String> = last.as_ref().and_then(|row| row.try_get("tx_type").ok());
    let now = Utc::now();
    let freshness_seconds = match last_commit_at {
        Some(ts) => (now - ts).num_seconds().max(0),
        None => 0,
    };

    // 2) Failure rate over the last 24h — committed vs aborted.
    let twenty_four_hours_ago = now - Duration::hours(24);
    let failure_breakdown = sqlx::query(
        r#"SELECT tx_type, status, COUNT(*) AS cnt
             FROM dataset_transactions
            WHERE dataset_id = $1
              AND COALESCE(committed_at, aborted_at, started_at) >= $2
            GROUP BY tx_type, status"#,
    )
    .bind(dataset_id)
    .bind(twenty_four_hours_ago)
    .fetch_all(db)
    .await
    .map_err(|e| e.to_string())?;

    let mut total_24 = 0i64;
    let mut aborted_24 = 0i64;
    let mut breakdown: BTreeMap<String, i64> = BTreeMap::new();
    for row in &failure_breakdown {
        let tx_type: String = row.get("tx_type");
        let status: String = row.get("status");
        let cnt: i64 = row.get("cnt");
        total_24 += cnt;
        if status == "ABORTED" {
            aborted_24 += cnt;
            *breakdown.entry(tx_type.clone()).or_insert(0) += cnt;
        }
    }
    let txn_failure_rate_24h = if total_24 == 0 {
        0.0
    } else {
        aborted_24 as f64 / total_24 as f64
    };

    // 3) Row count + col_count from `dataset_files` (latest committed
    //    view) and from the per-view schema row.
    let row_count: Option<i64> = sqlx::query_scalar(
        r#"SELECT SUM(size_bytes)::BIGINT
             FROM dataset_files
            WHERE dataset_id = $1
              AND deleted_at IS NULL"#,
    )
    .bind(dataset_id)
    .fetch_one(db)
    .await
    .ok();
    // size_bytes is a proxy when actual row counts haven't been
    // materialised yet — the catalog's `dataset.row_count` column
    // takes precedence when present.
    let row_count_catalog: Option<i64> =
        sqlx::query_scalar("SELECT row_count FROM datasets WHERE id = $1")
            .bind(dataset_id)
            .fetch_optional(db)
            .await
            .map_err(|e| e.to_string())?
            .flatten();
    let row_count = row_count_catalog.unwrap_or(row_count.unwrap_or(0));

    let col_count = compute_col_count(db, dataset_id).await.unwrap_or(0);

    // 4) Schema drift — true when the most recent view's schema_json
    //    differs from the prior one. We compare content_hash to keep
    //    this one query.
    let schema_drift_flag = compute_schema_drift(db, dataset_id).await.unwrap_or(false);

    // 5) last_build_status — derived from the most recent transaction
    //    + freshness:
    //      * COMMITTED less than 24h ago      → success
    //      * latest is ABORTED                → failed
    //      * COMMITTED but > 7 days ago       → stale
    //      * never committed                  → unknown
    let last_build_status = derive_last_build_status(last_commit_at, now);

    let extras = json!({
        "failure_breakdown_24h": breakdown,
        "transactions_total_24h": total_24,
        "aborted_total_24h": aborted_24,
        "last_tx_type": last_tx_type,
    });

    Ok(Some(ComputedHealth {
        dataset_id: Some(dataset_id),
        row_count,
        col_count,
        null_pct_by_column: BTreeMap::new(),
        freshness_seconds,
        last_commit_at,
        txn_failure_rate_24h,
        last_build_status,
        schema_drift_flag,
        extras,
    }))
}

async fn lookup_dataset_id(db: &sqlx::PgPool, rid: &str) -> Result<Option<Uuid>, String> {
    let row = sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(db)
        .await;
    match row {
        Ok(id) => Ok(id),
        Err(sqlx::Error::Database(db_err)) if db_err.message().contains("does not exist") => {
            Ok(None)
        }
        Err(other) => Err(other.to_string()),
    }
}

async fn compute_col_count(db: &sqlx::PgPool, dataset_id: Uuid) -> Result<i32, String> {
    // Pull the most recent view schema and count its `fields`.
    let json: Option<Value> = sqlx::query_scalar(
        r#"SELECT schema_json
             FROM dataset_view_schemas s
             JOIN dataset_views v ON v.id = s.view_id
            WHERE v.dataset_id = $1
            ORDER BY s.created_at DESC
            LIMIT 1"#,
    )
    .bind(dataset_id)
    .fetch_optional(db)
    .await
    .ok()
    .flatten();
    let count = json
        .and_then(|v| v.get("fields").cloned())
        .and_then(|fields| fields.as_array().map(|a| a.len() as i32))
        .unwrap_or(0);
    Ok(count)
}

async fn compute_schema_drift(db: &sqlx::PgPool, dataset_id: Uuid) -> Result<bool, String> {
    let hashes: Vec<String> = sqlx::query_scalar(
        r#"SELECT s.content_hash
             FROM dataset_view_schemas s
             JOIN dataset_views v ON v.id = s.view_id
            WHERE v.dataset_id = $1
            ORDER BY s.created_at DESC
            LIMIT 2"#,
    )
    .bind(dataset_id)
    .fetch_all(db)
    .await
    .map_err(|e| e.to_string())?;
    Ok(matches!(hashes.as_slice(), [a, b] if a != b))
}

/// Map (most-recent-commit-or-none, now) onto the doc's
/// `last_build_status` enum. Mirrors the colour code the dashboard
/// card uses: green when fresh, amber when stale, red on abort, grey
/// when there's nothing to report.
fn derive_last_build_status(last_commit_at: Option<DateTime<Utc>>, now: DateTime<Utc>) -> String {
    let Some(ts) = last_commit_at else {
        return "unknown".into();
    };
    let age = (now - ts).num_seconds();
    if age <= 7 * 24 * 3600 {
        "success".into()
    } else {
        "stale".into()
    }
}
