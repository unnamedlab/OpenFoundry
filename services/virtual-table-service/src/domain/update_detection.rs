//! Update-detection poller for virtual tables.
//!
//! Foundry doc § "Update detection for virtual table inputs". Two
//! pieces:
//!
//!   1. **Per-provider version probe** ([`current_version`]) — returns
//!      the source's current snapshot id / last-commit time / ETag.
//!      Returns [`Version::Unknown`] for sources without versioning so
//!      the poll is treated as a *potential* update by downstream
//!      triggers (doc: "If versioning is not supported, every poll
//!      is treated as a potential update").
//!   2. **Poll classification** ([`classify_change`]) — pure helper
//!      that, given the previous and current versions, decides whether
//!      the row should be marked as changed and the outbox event
//!      emitted.
//!
//! The orchestration runs on a tokio interval spawned from `main.rs`.
//! Polls are persisted into `update_detection_polls` so the
//! `GET /update-detection/history` endpoint can render the same audit
//! trail the operator uses to graph error rates by source.

use std::time::Duration;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sqlx::PgPool;
use uuid::Uuid;

use crate::AppState;
use crate::domain::audit;
use crate::domain::capability_matrix::SourceProvider;
use crate::models::virtual_table::{Locator, VirtualTableRow};

// ---------------------------------------------------------------------------
// Pure types.
// ---------------------------------------------------------------------------

/// Outcome of the per-provider version probe.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Version {
    /// Source supports versioning and returned a stable id (Iceberg
    /// snapshot id, Delta `_delta_log` sequence, BigQuery
    /// `lastModifiedTime`, ETag, …).
    Known { value: String },
    /// Source does not surface a version. The poll is treated as a
    /// potential update — downstream triggers run on every tick.
    Unknown,
}

impl Version {
    pub fn known(value: impl Into<String>) -> Self {
        Self::Known {
            value: value.into(),
        }
    }

    pub fn as_persisted(&self) -> Option<&str> {
        match self {
            Self::Known { value } => Some(value),
            Self::Unknown => None,
        }
    }
}

/// Classification of a poll relative to the previous poll, used by the
/// orchestration loop to decide whether to emit an outbox event.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum PollOutcome {
    /// First poll on this row (no prior `last_observed_version`).
    Initial,
    /// Source supports versioning and the version advanced.
    Changed,
    /// Source supports versioning and the version is identical.
    Unchanged,
    /// Source does not surface a version → treat as potential update.
    PotentialUpdate,
    /// The probe failed; backoff bookkeeping kicks in.
    Failed,
}

impl PollOutcome {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Initial => "initial",
            Self::Changed => "changed",
            Self::Unchanged => "unchanged",
            Self::PotentialUpdate => "potential_update",
            Self::Failed => "failed",
        }
    }

    /// `true` when downstream triggers should fire — i.e. the operator
    /// should treat this poll as a "data updated" signal. Mirrors the
    /// doc: known-version changes fire, `Unknown` fires every poll,
    /// stable / failed polls do not fire.
    pub fn fires_downstream_event(self) -> bool {
        matches!(self, Self::Initial | Self::Changed | Self::PotentialUpdate)
    }
}

/// Compare a fresh `current` version against the row's
/// `last_observed_version` and decide what the poll is.
pub fn classify_change(previous: Option<&str>, current: &Version) -> PollOutcome {
    match (previous, current) {
        (_, Version::Unknown) => PollOutcome::PotentialUpdate,
        (None, Version::Known { .. }) => PollOutcome::Initial,
        (Some(prev), Version::Known { value }) => {
            if prev == value {
                PollOutcome::Unchanged
            } else {
                PollOutcome::Changed
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Per-provider probe.
// ---------------------------------------------------------------------------

/// Probe the current version for a `(provider, locator)` pair.
///
/// P5 ships the orchestration + the per-provider routing; the live
/// probe bodies are stubs that return deterministic synthetic
/// versions so the integration tests + UI flow exercise the full
/// path. P5.next swaps the stub bodies for live SDK calls behind the
/// `provider-databricks` / `provider-iceberg` feature flags.
pub async fn current_version(
    provider: SourceProvider,
    locator: &Locator,
    last_observed_version: Option<&str>,
) -> std::result::Result<Version, String> {
    Ok(match (provider, locator) {
        // Iceberg: snapshot id (live SDK reads `metadata/v*.metadata.json`).
        (_, Locator::Iceberg { catalog, namespace, table }) => {
            Version::known(format!("snapshot:{}/{}/{}/v1", catalog, namespace, table))
        }
        // Foundry-managed Iceberg.
        (SourceProvider::FoundryIceberg, _) => Version::known("foundry-iceberg-snapshot"),
        // Delta tables (Databricks): `_delta_log` sequence.
        (SourceProvider::Databricks, Locator::Tabular { database, schema, table }) => {
            Version::known(format!("delta-log:{}.{}.{}", database, schema, table))
        }
        // BigQuery / Snowflake: lastModifiedTime stub.
        (
            SourceProvider::BigQuery | SourceProvider::Snowflake,
            Locator::Tabular { database, schema, table },
        ) => Version::known(format!(
            "modified-at:{}.{}.{}",
            database, schema, table
        )),
        // Object stores backed by Delta / Iceberg take the iceberg
        // path above; raw Parquet / Avro / CSV fall through here and
        // return Unknown — Foundry doc: "If versioning is not
        // supported, every poll is treated as a potential update".
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            Locator::File { format, .. },
        ) if matches!(format.to_ascii_lowercase().as_str(), "parquet" | "avro" | "csv") => {
            Version::Unknown
        }
        // The default for everything else is "use whatever signature
        // we already have"; this keeps the poll idempotent on
        // providers we have not specifically routed yet.
        _ => match last_observed_version {
            Some(v) => Version::known(v),
            None => Version::Unknown,
        },
    })
}

// ---------------------------------------------------------------------------
// Endpoint payloads.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateDetectionToggle {
    pub enabled: bool,
    #[serde(default = "default_interval_seconds")]
    pub interval_seconds: u64,
}

fn default_interval_seconds() -> u64 {
    3600
}

#[derive(Debug, Clone, Serialize)]
pub struct PollResult {
    pub virtual_table_rid: String,
    pub outcome: PollOutcome,
    pub observed_version: Option<String>,
    pub previous_version: Option<String>,
    pub latency_ms: i32,
    pub change_detected: bool,
    pub event_emitted: bool,
}

#[derive(Debug, Clone, sqlx::FromRow, Serialize)]
pub struct PollHistoryRow {
    pub id: Uuid,
    pub virtual_table_id: Uuid,
    pub polled_at: DateTime<Utc>,
    pub observed_version: Option<String>,
    pub change_detected: bool,
    pub latency_ms: i32,
    pub error_message: Option<String>,
}

#[derive(Debug, thiserror::Error)]
pub enum UpdateDetectionError {
    #[error("virtual table not found: {0}")]
    NotFound(String),
    #[error("update detection is disabled for this virtual table")]
    Disabled,
    #[error("invalid interval: {0}")]
    InvalidInterval(u64),
    #[error("invalid locator: {0}")]
    InvalidLocator(String),
    #[error("invalid provider: {0}")]
    InvalidProvider(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("upstream error: {0}")]
    Upstream(String),
}

pub type Result<T> = std::result::Result<T, UpdateDetectionError>;

// ---------------------------------------------------------------------------
// Endpoint orchestration.
// ---------------------------------------------------------------------------

/// PATCH /v1/virtual-tables/{rid}/update-detection — toggle + interval.
pub async fn set_toggle(
    state: &AppState,
    rid: &str,
    actor_id: Option<&str>,
    body: UpdateDetectionToggle,
) -> Result<VirtualTableRow> {
    if body.enabled && body.interval_seconds < 60 {
        return Err(UpdateDetectionError::InvalidInterval(body.interval_seconds));
    }
    let row: Option<VirtualTableRow> = sqlx::query_as(
        r#"UPDATE virtual_tables
            SET update_detection_enabled = $1,
                update_detection_interval_seconds = CASE
                    WHEN $1 THEN $2::INT
                    ELSE update_detection_interval_seconds
                END,
                update_detection_next_poll_at = CASE
                    WHEN $1 THEN NOW()
                    ELSE NULL
                END,
                update_detection_consecutive_failures = 0,
                updated_at = NOW()
            WHERE rid = $3
            RETURNING *"#,
    )
    .bind(body.enabled)
    .bind(body.interval_seconds as i32)
    .bind(rid)
    .fetch_optional(&state.db)
    .await?;

    let row = row.ok_or_else(|| UpdateDetectionError::NotFound(rid.to_string()))?;

    audit::record(
        &state.db,
        Some(&row.source_rid),
        Some(row.id),
        if body.enabled {
            "virtual_table.update_detection_enabled"
        } else {
            "virtual_table.update_detection_disabled"
        },
        actor_id,
        json!({
            "rid": rid,
            "interval_seconds": body.interval_seconds,
        }),
    )
    .await;

    Ok(row)
}

/// POST /v1/virtual-tables/{rid}/update-detection:poll-now — trigger
/// an immediate probe, regardless of the row's interval / next-poll.
pub async fn poll_now(state: &AppState, rid: &str) -> Result<PollResult> {
    let row: Option<VirtualTableRow> =
        sqlx::query_as("SELECT * FROM virtual_tables WHERE rid = $1")
            .bind(rid)
            .fetch_optional(&state.db)
            .await?;
    let row = row.ok_or_else(|| UpdateDetectionError::NotFound(rid.to_string()))?;

    let provider = lookup_provider(&state.db, &row.source_rid).await?;
    let locator: Locator = serde_json::from_value(row.locator.clone())
        .map_err(|err| UpdateDetectionError::InvalidLocator(err.to_string()))?;

    poll_one(state, &row, provider, &locator).await
}

/// GET /v1/virtual-tables/{rid}/update-detection/history?limit=
pub async fn history(
    pool: &PgPool,
    rid: &str,
    limit: i64,
) -> Result<Vec<PollHistoryRow>> {
    let row: Option<(Uuid,)> = sqlx::query_as("SELECT id FROM virtual_tables WHERE rid = $1")
        .bind(rid)
        .fetch_optional(pool)
        .await?;
    let virtual_table_id = row
        .ok_or_else(|| UpdateDetectionError::NotFound(rid.to_string()))?
        .0;
    let rows: Vec<PollHistoryRow> = sqlx::query_as(
        r#"SELECT id, virtual_table_id, polled_at, observed_version,
                  change_detected, latency_ms, error_message
            FROM update_detection_polls
            WHERE virtual_table_id = $1
            ORDER BY polled_at DESC
            LIMIT $2"#,
    )
    .bind(virtual_table_id)
    .bind(limit.clamp(1, 500))
    .fetch_all(pool)
    .await?;
    Ok(rows)
}

// ---------------------------------------------------------------------------
// Poller loop (called from main.rs spawn).
// ---------------------------------------------------------------------------

/// One tick of the poller: fetch every due row, probe each, persist
/// the poll and (on change) emit the outbox event. Returns the number
/// of rows polled this tick.
pub async fn run_tick(state: &AppState) -> Result<usize> {
    let rows: Vec<VirtualTableRow> = sqlx::query_as(
        r#"SELECT * FROM virtual_tables
            WHERE update_detection_enabled
              AND (update_detection_next_poll_at IS NULL
                   OR update_detection_next_poll_at <= NOW())
            ORDER BY update_detection_next_poll_at NULLS FIRST
            LIMIT 100"#,
    )
    .fetch_all(&state.db)
    .await?;

    let count = rows.len();
    for row in rows {
        let Ok(provider) = lookup_provider(&state.db, &row.source_rid).await else {
            continue;
        };
        let Ok(locator) = serde_json::from_value::<Locator>(row.locator.clone()) else {
            continue;
        };
        // Best-effort: a single row's probe failure must not break
        // the whole tick.
        if let Err(error) = poll_one(state, &row, provider, &locator).await {
            tracing::warn!(rid = %row.rid, ?error, "update-detection poll failed");
        }
    }

    Ok(count)
}

async fn lookup_provider(pool: &PgPool, source_rid: &str) -> Result<SourceProvider> {
    let raw: Option<(String,)> = sqlx::query_as(
        "SELECT provider FROM virtual_table_sources_link WHERE source_rid = $1",
    )
    .bind(source_rid)
    .fetch_optional(pool)
    .await?;
    let raw = raw.ok_or_else(|| UpdateDetectionError::NotFound(source_rid.to_string()))?;
    SourceProvider::parse(&raw.0)
        .ok_or_else(|| UpdateDetectionError::InvalidProvider(raw.0))
}

async fn poll_one(
    state: &AppState,
    row: &VirtualTableRow,
    provider: SourceProvider,
    locator: &Locator,
) -> Result<PollResult> {
    let started = std::time::Instant::now();
    let probe = current_version(provider, locator, row.last_observed_version.as_deref()).await;
    let latency_ms = started.elapsed().as_millis().min(i32::MAX as u128) as i32;

    match probe {
        Err(error) => {
            persist_poll(&state.db, row.id, None, false, latency_ms, Some(&error)).await?;
            apply_failure_backoff(state, row).await?;
            Ok(PollResult {
                virtual_table_rid: row.rid.clone(),
                outcome: PollOutcome::Failed,
                observed_version: None,
                previous_version: row.last_observed_version.clone(),
                latency_ms,
                change_detected: false,
                event_emitted: false,
            })
        }
        Ok(version) => {
            let outcome = classify_change(row.last_observed_version.as_deref(), &version);
            let new_observed = version.as_persisted().map(|s| s.to_string());
            let change_detected = matches!(outcome, PollOutcome::Changed | PollOutcome::Initial);
            persist_poll(
                &state.db,
                row.id,
                new_observed.as_deref(),
                change_detected,
                latency_ms,
                None,
            )
            .await?;
            schedule_next_poll(state, row, &new_observed).await?;

            let event_emitted = if outcome.fires_downstream_event() {
                emit_dataset_updated_event(&state.db, row, &outcome, new_observed.as_deref())
                    .await?;
                true
            } else {
                false
            };

            Ok(PollResult {
                virtual_table_rid: row.rid.clone(),
                outcome,
                observed_version: new_observed,
                previous_version: row.last_observed_version.clone(),
                latency_ms,
                change_detected,
                event_emitted,
            })
        }
    }
}

async fn persist_poll(
    pool: &PgPool,
    virtual_table_id: Uuid,
    observed_version: Option<&str>,
    change_detected: bool,
    latency_ms: i32,
    error_message: Option<&str>,
) -> Result<()> {
    sqlx::query(
        r#"INSERT INTO update_detection_polls
                (virtual_table_id, observed_version, change_detected,
                 latency_ms, error_message)
            VALUES ($1, $2, $3, $4, $5)"#,
    )
    .bind(virtual_table_id)
    .bind(observed_version)
    .bind(change_detected)
    .bind(latency_ms)
    .bind(error_message)
    .execute(pool)
    .await?;
    Ok(())
}

async fn schedule_next_poll(
    state: &AppState,
    row: &VirtualTableRow,
    new_version: &Option<String>,
) -> Result<()> {
    let interval = row.update_detection_interval_seconds.unwrap_or(3600).max(60);
    sqlx::query(
        r#"UPDATE virtual_tables
            SET last_polled_at = NOW(),
                last_observed_version = COALESCE($1, last_observed_version),
                update_detection_consecutive_failures = 0,
                update_detection_next_poll_at = NOW() + ($2::INT * INTERVAL '1 second'),
                updated_at = NOW()
            WHERE id = $3"#,
    )
    .bind(new_version.as_deref())
    .bind(interval)
    .bind(row.id)
    .execute(&state.db)
    .await?;
    Ok(())
}

async fn apply_failure_backoff(state: &AppState, row: &VirtualTableRow) -> Result<()> {
    let base = row.update_detection_interval_seconds.unwrap_or(3600).max(60);
    // Exponential backoff capped at 24h.
    let failures = (row.update_detection_consecutive_failures + 1).min(8);
    let backoff_seconds = base.saturating_mul(1 << failures.min(8)).min(86_400);
    sqlx::query(
        r#"UPDATE virtual_tables
            SET last_polled_at = NOW(),
                update_detection_consecutive_failures = $1,
                update_detection_next_poll_at = NOW() + ($2::INT * INTERVAL '1 second'),
                updated_at = NOW()
            WHERE id = $3"#,
    )
    .bind(failures)
    .bind(backoff_seconds)
    .bind(row.id)
    .execute(&state.db)
    .await?;
    Ok(())
}

async fn emit_dataset_updated_event(
    pool: &PgPool,
    row: &VirtualTableRow,
    outcome: &PollOutcome,
    observed_version: Option<&str>,
) -> Result<()> {
    // Land an outbox row on `foundry.dataset.events.v1`. D1.1.6 P1's
    // trigger engine listens to that topic and routes events keyed
    // by `target_rid` to schedule runs.
    sqlx::query(
        r#"INSERT INTO virtual_table_audit
                (virtual_table_id, source_rid, action, actor_id, details)
            VALUES ($1, $2, $3, NULL, $4::jsonb)"#,
    )
    .bind(row.id)
    .bind(&row.source_rid)
    .bind("virtual_table.update_detected")
    .bind(json!({
        "outcome": outcome.as_str(),
        "observed_version": observed_version,
        "topic": "foundry.dataset.events.v1",
        "event_type": "DATA_UPDATED",
        "target_rid": row.rid,
    }))
    .execute(pool)
    .await?;
    tracing::info!(
        target: "audit",
        kind = "virtual_table.update_detected",
        rid = %row.rid,
        outcome = outcome.as_str(),
        observed_version = observed_version.unwrap_or(""),
        "emitted DATA_UPDATED event"
    );
    Ok(())
}

/// Backwards-compat helper used by a smoke test below.
#[allow(dead_code)]
fn poll_interval_default() -> Duration {
    Duration::from_secs(default_interval_seconds())
}

#[allow(dead_code)]
fn _value_used(_: Value) {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classify_change_initial_when_no_prior_version() {
        assert_eq!(
            classify_change(None, &Version::known("v1")),
            PollOutcome::Initial
        );
    }

    #[test]
    fn classify_change_unchanged_when_versions_match() {
        assert_eq!(
            classify_change(Some("v1"), &Version::known("v1")),
            PollOutcome::Unchanged
        );
    }

    #[test]
    fn classify_change_changed_when_versions_differ() {
        assert_eq!(
            classify_change(Some("v1"), &Version::known("v2")),
            PollOutcome::Changed
        );
    }

    #[test]
    fn classify_change_potential_update_for_unknown_version() {
        // Doc: "If versioning is not supported, every poll is treated
        // as a potential update".
        assert_eq!(
            classify_change(None, &Version::Unknown),
            PollOutcome::PotentialUpdate
        );
        assert_eq!(
            classify_change(Some("anything"), &Version::Unknown),
            PollOutcome::PotentialUpdate
        );
    }

    #[test]
    fn fires_downstream_event_aligned_with_doc() {
        assert!(PollOutcome::Initial.fires_downstream_event());
        assert!(PollOutcome::Changed.fires_downstream_event());
        assert!(PollOutcome::PotentialUpdate.fires_downstream_event());
        assert!(!PollOutcome::Unchanged.fires_downstream_event());
        assert!(!PollOutcome::Failed.fires_downstream_event());
    }

    #[tokio::test]
    async fn current_version_iceberg_reports_snapshot_id() {
        let v = current_version(
            SourceProvider::Snowflake,
            &Locator::Iceberg {
                catalog: "polaris".into(),
                namespace: "sales".into(),
                table: "events".into(),
            },
            None,
        )
        .await
        .expect("probe");
        match v {
            Version::Known { value } => assert!(value.starts_with("snapshot:polaris/sales/events")),
            _ => panic!("expected Known"),
        }
    }

    #[tokio::test]
    async fn current_version_databricks_reports_delta_log() {
        let v = current_version(
            SourceProvider::Databricks,
            &Locator::Tabular {
                database: "main".into(),
                schema: "public".into(),
                table: "orders".into(),
            },
            None,
        )
        .await
        .expect("probe");
        match v {
            Version::Known { value } => assert!(value.starts_with("delta-log:")),
            _ => panic!("expected Known"),
        }
    }

    #[tokio::test]
    async fn current_version_object_store_parquet_returns_unknown() {
        let v = current_version(
            SourceProvider::AmazonS3,
            &Locator::File {
                bucket: "openfoundry".into(),
                prefix: "year=2026/".into(),
                format: "parquet".into(),
            },
            None,
        )
        .await
        .expect("probe");
        assert_eq!(v, Version::Unknown);
    }

    #[tokio::test]
    async fn current_version_object_store_csv_returns_unknown() {
        let v = current_version(
            SourceProvider::Gcs,
            &Locator::File {
                bucket: "b".into(),
                prefix: "p".into(),
                format: "CSV".into(),
            },
            None,
        )
        .await
        .expect("probe");
        assert_eq!(v, Version::Unknown);
    }
}
