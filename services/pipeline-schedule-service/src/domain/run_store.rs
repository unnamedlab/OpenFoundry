//! CRUD over `schedule_runs` — one row per dispatch attempt, with the
//! Foundry-doc outcome enum (Succeeded / Ignored / Failed).
//!
//! The dispatcher writes here when it decides the outcome of a run;
//! the auto-pause supervisor reads back the most recent rows to count
//! consecutive failures. Both paths use the typed [`RunOutcome`] enum
//! so the database CHECK and the in-memory match arms can never drift.

use std::collections::BTreeMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{PgPool, Row, postgres::PgRow};
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum RunOutcome {
    Succeeded,
    Ignored,
    Failed,
}

impl RunOutcome {
    pub fn as_str(&self) -> &'static str {
        match self {
            RunOutcome::Succeeded => "SUCCEEDED",
            RunOutcome::Ignored => "IGNORED",
            RunOutcome::Failed => "FAILED",
        }
    }

    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "SUCCEEDED" => Some(RunOutcome::Succeeded),
            "IGNORED" => Some(RunOutcome::Ignored),
            "FAILED" => Some(RunOutcome::Failed),
            _ => None,
        }
    }
}

/// In-memory mirror of a `schedule_runs` row.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ScheduleRun {
    pub id: Uuid,
    pub rid: String,
    pub schedule_id: Uuid,
    pub outcome: RunOutcome,
    pub build_rid: Option<String>,
    pub failure_reason: Option<String>,
    pub triggered_at: DateTime<Utc>,
    pub finished_at: Option<DateTime<Utc>>,
    pub trigger_snapshot: BTreeMap<String, String>,
    pub schedule_version: i32,
}

#[derive(Debug, thiserror::Error)]
pub enum RunStoreError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("invalid trigger snapshot: {0}")]
    InvalidSnapshot(serde_json::Error),
    #[error("invalid outcome '{0}'")]
    InvalidOutcome(String),
}

#[derive(Debug, Clone)]
pub struct InsertRun {
    pub schedule_id: Uuid,
    pub outcome: RunOutcome,
    pub build_rid: Option<String>,
    pub failure_reason: Option<String>,
    pub trigger_snapshot: BTreeMap<String, String>,
    pub schedule_version: i32,
    /// `true` when the dispatcher knows the dispatch is finished (i.e.
    /// IGNORED / FAILED never start a build, so finished_at is set
    /// immediately). For SUCCEEDED runs the field can be filled later.
    pub finished_now: bool,
}

pub async fn insert_run(pool: &PgPool, req: InsertRun) -> Result<ScheduleRun, RunStoreError> {
    let id = Uuid::now_v7();
    let snapshot_value =
        serde_json::to_value(&req.trigger_snapshot).map_err(RunStoreError::InvalidSnapshot)?;
    let row = sqlx::query(
        r#"INSERT INTO schedule_runs (
                id, schedule_id, outcome, build_rid, failure_reason,
                triggered_at, finished_at, trigger_snapshot, schedule_version
           ) VALUES ($1, $2, $3, $4, $5, NOW(),
                     CASE WHEN $6 THEN NOW() ELSE NULL END,
                     $7, $8)
           RETURNING id, rid, schedule_id, outcome, build_rid, failure_reason,
                     triggered_at, finished_at, trigger_snapshot, schedule_version"#,
    )
    .bind(id)
    .bind(req.schedule_id)
    .bind(req.outcome.as_str())
    .bind(&req.build_rid)
    .bind(&req.failure_reason)
    .bind(req.finished_now)
    .bind(snapshot_value)
    .bind(req.schedule_version)
    .fetch_one(pool)
    .await?;
    run_from_row(&row)
}

pub async fn finish_run(
    pool: &PgPool,
    run_id: Uuid,
    finished_at: DateTime<Utc>,
) -> Result<(), RunStoreError> {
    sqlx::query(
        "UPDATE schedule_runs SET finished_at = $1 WHERE id = $2 AND finished_at IS NULL",
    )
    .bind(finished_at)
    .bind(run_id)
    .execute(pool)
    .await?;
    Ok(())
}

#[derive(Debug, Clone, Default)]
pub struct ListRunsFilter {
    pub outcome: Option<RunOutcome>,
    pub limit: i64,
    pub offset: i64,
}

pub async fn list_for_schedule(
    pool: &PgPool,
    schedule_id: Uuid,
    filter: ListRunsFilter,
) -> Result<Vec<ScheduleRun>, RunStoreError> {
    let limit = if filter.limit <= 0 { 50 } else { filter.limit.min(500) };
    let offset = filter.offset.max(0);
    let rows = sqlx::query(
        r#"SELECT id, rid, schedule_id, outcome, build_rid, failure_reason,
                  triggered_at, finished_at, trigger_snapshot, schedule_version
             FROM schedule_runs
            WHERE schedule_id = $1
              AND ($2::TEXT IS NULL OR outcome = $2)
            ORDER BY triggered_at DESC
            LIMIT $3 OFFSET $4"#,
    )
    .bind(schedule_id)
    .bind(filter.outcome.map(|o| o.as_str().to_string()))
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;
    rows.iter().map(run_from_row).collect()
}

/// Return the `n` most-recent run outcomes for a schedule, in
/// descending order by `triggered_at`. Used by the auto-pause
/// supervisor to count consecutive FAILED runs without pulling whole
/// rows.
pub async fn last_outcomes(
    pool: &PgPool,
    schedule_id: Uuid,
    n: i64,
) -> Result<Vec<RunOutcome>, RunStoreError> {
    let rows: Vec<(String,)> = sqlx::query_as(
        "SELECT outcome FROM schedule_runs
          WHERE schedule_id = $1
          ORDER BY triggered_at DESC
          LIMIT $2",
    )
    .bind(schedule_id)
    .bind(n)
    .fetch_all(pool)
    .await?;
    rows.into_iter()
        .map(|(s,)| RunOutcome::parse(&s).ok_or_else(|| RunStoreError::InvalidOutcome(s)))
        .collect()
}

fn run_from_row(row: &PgRow) -> Result<ScheduleRun, RunStoreError> {
    let outcome_str: String = row.try_get("outcome")?;
    let outcome = RunOutcome::parse(&outcome_str)
        .ok_or_else(|| RunStoreError::InvalidOutcome(outcome_str.clone()))?;
    let snapshot_value: Value = row.try_get("trigger_snapshot")?;
    let trigger_snapshot: BTreeMap<String, String> =
        serde_json::from_value(snapshot_value).map_err(RunStoreError::InvalidSnapshot)?;
    Ok(ScheduleRun {
        id: row.try_get("id")?,
        rid: row.try_get("rid")?,
        schedule_id: row.try_get("schedule_id")?,
        outcome,
        build_rid: row.try_get("build_rid")?,
        failure_reason: row.try_get("failure_reason")?,
        triggered_at: row.try_get("triggered_at")?,
        finished_at: row.try_get("finished_at")?,
        trigger_snapshot,
        schedule_version: row.try_get("schedule_version")?,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn outcome_round_trip_strings() {
        for (s, o) in [
            ("SUCCEEDED", RunOutcome::Succeeded),
            ("IGNORED", RunOutcome::Ignored),
            ("FAILED", RunOutcome::Failed),
        ] {
            assert_eq!(o.as_str(), s);
            assert_eq!(RunOutcome::parse(s), Some(o));
        }
        assert!(RunOutcome::parse("BAD").is_none());
    }
}
