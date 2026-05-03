//! Hourly archive worker for branch retention.
//!
//! Mirrors the Foundry "Branch retention" doc § "Inactive branches
//! get archived automatically": every hour the worker scans
//! `dataset_branches` for non-root, non-archived branches with no
//! OPEN transaction whose effective TTL has lapsed, soft-archives
//! them (and reparents their children to the grandparent), and emits
//! a `dataset.branch.archived.v1` outbox event per archive.
//!
//! `INHERITED` resolves up the parent chain via
//! [`super::retention::resolve_effective_retention`] so the worker
//! sees the same effective policy the UI surfaces.

use std::collections::HashMap;

use chrono::{DateTime, Duration, Utc};
use serde_json::json;
use sqlx::PgPool;
use uuid::Uuid;

use crate::domain::branch_events::{self, BranchEnvelope};
use crate::domain::retention::{
    EffectiveRetention, RetentionPolicy, RetentionRow, is_archive_eligible,
    resolve_effective_retention,
};

/// Restore-grace window after archive — see `handlers::retention`.
const ARCHIVE_GRACE_DAYS: i64 = 7;

/// Archive every eligible branch in the database. Returns the number
/// of branches archived. Used by both the hourly worker and the
/// integration tests (the test calls `run_once` directly to avoid
/// waiting for the schedule).
pub async fn run_once(pool: &PgPool) -> Result<usize, sqlx::Error> {
    let now = Utc::now();
    let rows = load_active_rows(pool).await?;
    let index: HashMap<Uuid, RetentionRow> = rows.iter().cloned().map(|r| (r.id, r)).collect();

    let mut archived = 0usize;
    for row in &rows {
        let effective = resolve_effective_retention(row, &index);
        if !is_archive_eligible(row, &effective, now) {
            continue;
        }
        if archive_branch(pool, row, &effective, now, "ttl", "system").await? {
            archived += 1;
        }
    }

    crate::metrics::DATASET_BRANCHES_ARCHIVE_ELIGIBLE
        .set(eligible_gauge(&rows, &index, now) as i64);

    Ok(archived)
}

fn eligible_gauge(
    rows: &[RetentionRow],
    index: &HashMap<Uuid, RetentionRow>,
    now: DateTime<Utc>,
) -> usize {
    rows.iter()
        .filter(|r| {
            let eff = resolve_effective_retention(r, index);
            is_archive_eligible(r, &eff, now)
        })
        .count()
}

async fn load_active_rows(pool: &PgPool) -> Result<Vec<RetentionRow>, sqlx::Error> {
    let raw: Vec<(Uuid, Option<Uuid>, String, Option<i32>, DateTime<Utc>, Option<DateTime<Utc>>)> =
        sqlx::query_as(
            r#"SELECT b.id,
                      b.parent_branch_id,
                      b.retention_policy,
                      b.retention_ttl_days,
                      b.last_activity_at,
                      b.archived_at
                 FROM dataset_branches b
                WHERE b.deleted_at IS NULL"#,
        )
        .fetch_all(pool)
        .await?;

    let with_open_tx: std::collections::HashSet<Uuid> = sqlx::query_scalar::<_, Uuid>(
        r#"SELECT DISTINCT branch_id FROM dataset_transactions WHERE status = 'OPEN'"#,
    )
    .fetch_all(pool)
    .await?
    .into_iter()
    .collect();

    Ok(raw
        .into_iter()
        .map(|(id, parent, policy, ttl, last_activity_at, archived_at)| RetentionRow {
            id,
            parent_branch_id: parent,
            policy: RetentionPolicy::parse(&policy).unwrap_or(RetentionPolicy::Inherited),
            ttl_days: ttl,
            last_activity_at,
            has_open_transaction: with_open_tx.contains(&id),
            is_root: parent.is_none(),
            archived_at,
        })
        .collect())
}

/// Archive a single branch + reparent its direct children + emit the
/// outbox event. Returns `true` when the row was archived (idempotent
/// guard — already-archived rows return `false`).
pub async fn archive_branch(
    pool: &PgPool,
    row: &RetentionRow,
    effective: &EffectiveRetention,
    now: DateTime<Utc>,
    reason: &str,
    actor: &str,
) -> Result<bool, sqlx::Error> {
    let mut tx = pool.begin().await?;

    // Reparent direct children to the grandparent (which may be
    // `NULL` => they become root branches per Foundry guarantees).
    sqlx::query(
        r#"UPDATE dataset_branches
              SET parent_branch_id = $2, updated_at = NOW()
            WHERE parent_branch_id = $1 AND deleted_at IS NULL"#,
    )
    .bind(row.id)
    .bind(row.parent_branch_id)
    .execute(&mut *tx)
    .await?;

    // Soft-archive (keeps transactions intact per the doc).
    let updated = sqlx::query(
        r#"UPDATE dataset_branches
              SET archived_at = $2,
                  archive_grace_until = $3,
                  updated_at = NOW()
            WHERE id = $1 AND archived_at IS NULL"#,
    )
    .bind(row.id)
    .bind(now)
    .bind(now + Duration::days(ARCHIVE_GRACE_DAYS))
    .execute(&mut *tx)
    .await?
    .rows_affected();
    if updated == 0 {
        tx.rollback().await?;
        return Ok(false);
    }

    let (branch_rid, dataset_rid, parent_branch_id, head_id) = sqlx::query_as::<_, (String, String, Option<Uuid>, Option<Uuid>)>(
        r#"SELECT rid, dataset_rid, parent_branch_id, head_transaction_id
             FROM dataset_branches WHERE id = $1"#,
    )
    .bind(row.id)
    .fetch_one(&mut *tx)
    .await?;

    let envelope = BranchEnvelope::new(
        branch_events::EVT_ARCHIVED,
        &branch_rid,
        &dataset_rid,
        actor,
    )
    .with_parent_rid(parent_branch_id.map(|id| format!("ri.foundry.main.branch.{id}")))
    .with_head(head_id.map(|id| format!("ri.foundry.main.transaction.{id}")))
    .with_extras(json!({
        "reason": reason,
        "policy": effective.policy.as_str(),
        "ttl_days": effective.ttl_days,
    }));
    branch_events::emit(&mut tx, &envelope)
        .await
        .map_err(|e| sqlx::Error::Protocol(e.to_string()))?;

    tx.commit().await?;
    crate::metrics::DATASET_BRANCHES_ARCHIVED_TOTAL
        .with_label_values(&[reason])
        .inc();
    Ok(true)
}

/// Spawn the hourly archive loop. The future never returns under
/// normal operation; callers (typically `main.rs`) `tokio::spawn` it.
pub async fn run_loop(pool: PgPool) {
    let mut ticker = tokio::time::interval(std::time::Duration::from_secs(3600));
    // First tick fires immediately — skip it so a fresh restart
    // doesn't archive everything before the operator has a chance to
    // inspect the queue.
    ticker.tick().await;
    loop {
        ticker.tick().await;
        match run_once(&pool).await {
            Ok(n) => tracing::info!(archived = n, "branch retention worker tick"),
            Err(error) => tracing::warn!(%error, "branch retention worker error"),
        }
    }
}
