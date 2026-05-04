//! Cron-driven Kafka event emitter for the Foundry-Schedule trigger
//! pattern (ADR-0037, Tarea 1.3).
//!
//! ## What this crate is
//!
//! A small library that lets a single K8s `CronJob` pod replace the
//! ad-hoc in-process tick loops that services like
//! `automation-operations-service` and `workflow-automation-service`
//! used to run for time-based triggers. The pod runs the
//! `schedules-tick` binary every minute; the binary calls
//! [`Scheduler::tick`] once and exits with the number of events
//! emitted.
//!
//! ## Operating model
//!
//! 1. Operators populate `schedules.definitions` (see
//!    `migrations/0001_schedules_definitions.sql`) with one row per
//!    scheduled trigger — cron expression, IANA time zone, Kafka
//!    topic, and a verbatim JSON payload to publish.
//! 2. `Scheduler::tick(now)` claims every `enabled` row whose
//!    `next_run_at <= now` using `SELECT … FOR UPDATE SKIP LOCKED`,
//!    publishes the payload to its topic via [`event_bus_data`], and
//!    updates `next_run_at`/`last_run_at` inside the same
//!    transaction. The `SKIP LOCKED` clause makes overlapping ticks
//!    safe — at most one runner ever fires a given row per due
//!    instant.
//! 3. The runner relies on the in-house [`scheduling_cron`] parser
//!    (Foundry-parity Unix-5 / Quartz-6, IANA TZ, DST-correct), so it
//!    matches the rest of the platform's cron semantics rather than
//!    the looser semantics of the external `cron` crate.
//!
//! ## Delivery semantics
//!
//! Each fire is one Kafka record published with [`event_bus_data`]'s
//! at-least-once `acks=all` producer. The Kafka key is the schedule
//! `name`, which gives natural per-schedule ordering on the broker;
//! the OpenLineage `run_id` is a deterministic v5 UUID over
//! `(name, scheduled_for)` so a re-fire (e.g. operator manually
//! reset `next_run_at`) carries an id consumers can de-duplicate
//! against. If the Kafka publish fails, the surrounding transaction
//! rolls back and the row remains "due", so the next tick will retry
//! it; we never silently drop fires.

use std::sync::Arc;

use chrono::{DateTime, Utc};
use chrono_tz::Tz;
use event_bus_data::{DataPublisher, OpenLineageHeaders, PublishError};
use scheduling_cron::{CronError, CronFlavor, parse_cron};
use serde::{Deserialize, Serialize};
use sqlx::{PgPool, Row};
use thiserror::Error;
use uuid::Uuid;

// Re-export so binaries / consumers can build a publisher / pool of
// the right shape without an extra dependency line.
pub use event_bus_data;

// ─── Errors ───────────────────────────────────────────────────────────────

/// Errors raised by [`Scheduler::tick`] and helpers.
#[derive(Debug, Error)]
pub enum SchedulerError {
    /// Underlying Postgres error.
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),

    /// Kafka publish failed for the row identified by `name`.
    /// Boxed because `PublishError` (specifically its `KafkaError`
    /// variant) is large enough to bloat every `Result` return on
    /// the hot path.
    #[error("publish to topic `{topic}` failed for schedule `{name}`: {source}")]
    Publish {
        name: String,
        topic: String,
        #[source]
        source: Box<PublishError>,
    },

    /// `cron_expr` could not be parsed for the row identified by `name`.
    #[error("invalid cron expression for schedule `{name}`: {source}")]
    InvalidCron {
        name: String,
        #[source]
        source: CronError,
    },

    /// `cron_flavor` column held an unrecognised value.
    #[error("unknown cron flavor `{flavor}` for schedule `{name}` (expected `unix5` or `quartz6`)")]
    UnknownFlavor { name: String, flavor: String },

    /// `time_zone` column was not a valid IANA zone name.
    #[error("invalid time zone `{tz}` for schedule `{name}`")]
    InvalidTimeZone { name: String, tz: String },

    /// `next_fire_after` returned `None` — the cron expression has no
    /// matching instant within the evaluator's 10-year horizon.
    #[error("schedule `{name}` has no future fire within 10 years (cron: `{cron_expr}`)")]
    NoFutureFire { name: String, cron_expr: String },
}

// ─── Row model ────────────────────────────────────────────────────────────

/// A single row of `schedules.definitions`, in the shape the runner
/// uses internally. Surfaced publicly so callers (admin tools,
/// custom-endpoint code) can read/write rows without re-deriving the
/// shape.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleDefinition {
    pub id: Uuid,
    pub name: String,
    pub cron_expr: String,
    pub cron_flavor: String,
    pub time_zone: String,
    pub enabled: bool,
    pub topic: String,
    pub payload_template: serde_json::Value,
    pub next_run_at: DateTime<Utc>,
    pub last_run_at: Option<DateTime<Utc>>,
}

impl ScheduleDefinition {
    fn try_flavor(&self) -> Result<CronFlavor, SchedulerError> {
        match self.cron_flavor.as_str() {
            "unix5" => Ok(CronFlavor::Unix5),
            "quartz6" => Ok(CronFlavor::Quartz6),
            other => Err(SchedulerError::UnknownFlavor {
                name: self.name.clone(),
                flavor: other.to_string(),
            }),
        }
    }

    fn try_tz(&self) -> Result<Tz, SchedulerError> {
        self.time_zone
            .parse::<Tz>()
            .map_err(|_| SchedulerError::InvalidTimeZone {
                name: self.name.clone(),
                tz: self.time_zone.clone(),
            })
    }
}

// ─── Scheduler ────────────────────────────────────────────────────────────

/// OpenLineage namespace under which every emitted event is reported.
/// Surfaced as a `const` so consumers can subscribe by namespace.
pub const SCHEDULER_LINEAGE_NAMESPACE: &str = "of://schedules";

/// Producer URI advertised in the OpenLineage `producer` header.
pub const SCHEDULER_LINEAGE_PRODUCER: &str =
    "https://github.com/unnamedlab/OpenFoundry/libs/event-scheduler";

/// Cron-driven scheduler. Owns a Postgres pool and a Kafka publisher;
/// stateless across `tick()` calls so a binary can build it once and
/// invoke `tick` exactly once per K8s CronJob run.
pub struct Scheduler {
    pg: PgPool,
    publisher: Arc<dyn DataPublisher>,
}

impl Scheduler {
    /// Build a scheduler from a Postgres pool and a `DataPublisher`.
    pub fn new(pg: PgPool, publisher: Arc<dyn DataPublisher>) -> Self {
        Self { pg, publisher }
    }

    /// Postgres pool, exposed for tests / shutdown.
    pub fn pool(&self) -> &PgPool {
        &self.pg
    }

    /// Run one tick: claim every due, enabled schedule, publish its
    /// payload, advance its `next_run_at`, and stamp `last_run_at`.
    ///
    /// Returns the number of schedules that successfully fired. A row
    /// whose Kafka publish or cron-recompute fails causes the whole
    /// tick to abort with the corresponding [`SchedulerError`] so the
    /// CronJob pod restarts and retries; partial progress that
    /// already committed (because we commit per-row) is preserved.
    pub async fn tick(&self, now: DateTime<Utc>) -> Result<usize, SchedulerError> {
        let mut fired = 0usize;
        loop {
            // Claim and process one row at a time so a slow Kafka
            // publish can't hold a transaction open across many rows
            // (which would also extend the SKIP LOCKED window).
            let mut tx = self.pg.begin().await?;

            let row_opt = sqlx::query(
                "SELECT id, name, cron_expr, cron_flavor, time_zone, enabled, topic, \
                        payload_template, next_run_at, last_run_at \
                 FROM schedules.definitions \
                 WHERE enabled AND next_run_at <= $1 \
                 ORDER BY next_run_at \
                 LIMIT 1 \
                 FOR UPDATE SKIP LOCKED",
            )
            .bind(now)
            .fetch_optional(&mut *tx)
            .await?;

            let row = match row_opt {
                Some(r) => r,
                None => {
                    // Nothing more to do.
                    tx.commit().await?;
                    break;
                }
            };

            let def = ScheduleDefinition {
                id: row.try_get("id")?,
                name: row.try_get("name")?,
                cron_expr: row.try_get("cron_expr")?,
                cron_flavor: row.try_get("cron_flavor")?,
                time_zone: row.try_get("time_zone")?,
                enabled: row.try_get("enabled")?,
                topic: row.try_get("topic")?,
                payload_template: row.try_get("payload_template")?,
                next_run_at: row.try_get("next_run_at")?,
                last_run_at: row.try_get("last_run_at")?,
            };

            // The instant for which we are firing — record both in
            // OpenLineage and in `last_run_at` so consumers can tell
            // apart "fired late" from "fired on time".
            let scheduled_for = def.next_run_at;

            // Recompute `next_run_at` strictly past `now` so:
            //   * a schedule that was due exactly at `now` doesn't
            //     immediately re-fire inside the same tick loop, and
            //   * a tick that runs late (e.g. CronJob skipped a
            //     period) collapses any missed fires into one event
            //     and resumes from the next future slot — the
            //     standard cron / K8s `concurrencyPolicy=Forbid`
            //     semantic.
            // A malformed row can't slip through: if recompute fails
            // we abort the tick before publishing.
            let next = compute_next_fire(&def, now)?;

            // Publish to Kafka. We hold the row lock open across the
            // publish on purpose: if the broker is unavailable we
            // want the row to remain claimed only as long as the
            // publish attempt itself, then released untouched on
            // rollback so the next tick retries.
            let payload_bytes =
                serde_json::to_vec(&def.payload_template).map_err(|e| SchedulerError::Publish {
                    name: def.name.clone(),
                    topic: def.topic.clone(),
                    source: Box::new(PublishError::InvalidRecord(format!(
                        "payload_template is not serialisable: {e}"
                    ))),
                })?;

            let lineage = build_lineage(&def.name, scheduled_for);
            self.publisher
                .publish(
                    &def.topic,
                    Some(def.name.as_bytes()),
                    &payload_bytes,
                    &lineage,
                )
                .await
                .map_err(|source| SchedulerError::Publish {
                    name: def.name.clone(),
                    topic: def.topic.clone(),
                    source: Box::new(source),
                })?;

            sqlx::query(
                "UPDATE schedules.definitions \
                 SET next_run_at = $1, last_run_at = $2, updated_at = now() \
                 WHERE id = $3",
            )
            .bind(next)
            .bind(scheduled_for)
            .bind(def.id)
            .execute(&mut *tx)
            .await?;

            tx.commit().await?;

            tracing::info!(
                schedule = %def.name,
                topic = %def.topic,
                scheduled_for = %scheduled_for,
                next_run_at = %next,
                "schedule fired"
            );
            fired += 1;
        }
        Ok(fired)
    }
}

// ─── Pure helpers (no IO) ─────────────────────────────────────────────────

/// Build the OpenLineage headers for a single fire. Public so tests /
/// downstream consumers can recompute the deterministic `run_id` for
/// idempotent reprocessing.
pub fn build_lineage(name: &str, scheduled_for: DateTime<Utc>) -> OpenLineageHeaders {
    let key = format!("{name}|{}", scheduled_for.to_rfc3339());
    let run_id = Uuid::new_v5(&Uuid::NAMESPACE_OID, key.as_bytes()).to_string();
    OpenLineageHeaders::new(
        SCHEDULER_LINEAGE_NAMESPACE,
        name,
        run_id,
        SCHEDULER_LINEAGE_PRODUCER,
    )
    .with_event_time(scheduled_for)
}

/// Compute the next UTC instant strictly after `scheduled_for` at
/// which `def` should fire. Public so tests and admin tools can
/// reuse the same logic without going through `tick`.
pub fn compute_next_fire(
    def: &ScheduleDefinition,
    scheduled_for: DateTime<Utc>,
) -> Result<DateTime<Utc>, SchedulerError> {
    let flavor = def.try_flavor()?;
    let tz = def.try_tz()?;
    let schedule =
        parse_cron(&def.cron_expr, flavor, tz).map_err(|source| SchedulerError::InvalidCron {
            name: def.name.clone(),
            source,
        })?;
    scheduling_cron::next_fire_after(&schedule, scheduled_for).ok_or_else(|| {
        SchedulerError::NoFutureFire {
            name: def.name.clone(),
            cron_expr: def.cron_expr.clone(),
        }
    })
}

// ─── Unit tests ───────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;
    use serde_json::json;

    fn def(cron_expr: &str) -> ScheduleDefinition {
        ScheduleDefinition {
            id: Uuid::nil(),
            name: "demo".to_string(),
            cron_expr: cron_expr.to_string(),
            cron_flavor: "unix5".to_string(),
            time_zone: "UTC".to_string(),
            enabled: true,
            topic: "of.schedules.demo".to_string(),
            payload_template: json!({"hello": "world"}),
            next_run_at: Utc.with_ymd_and_hms(2026, 1, 1, 0, 0, 0).unwrap(),
            last_run_at: None,
        }
    }

    #[test]
    fn compute_next_fire_advances_minute_cron() {
        let d = def("*/5 * * * *"); // every 5 minutes
        let after = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 0).unwrap();
        let next = compute_next_fire(&d, after).expect("next");
        assert_eq!(next, Utc.with_ymd_and_hms(2026, 5, 4, 12, 5, 0).unwrap());
    }

    #[test]
    fn compute_next_fire_respects_quartz6_seconds_field() {
        let mut d = def("0 * * * * *");
        d.cron_flavor = "quartz6".to_string();
        let after = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 30).unwrap();
        let next = compute_next_fire(&d, after).expect("next");
        assert_eq!(next, Utc.with_ymd_and_hms(2026, 5, 4, 12, 1, 0).unwrap());
    }

    #[test]
    fn compute_next_fire_uses_iana_time_zone() {
        // Daily at 09:00 New York time. After 2026-05-04 12:00 UTC
        // (= 08:00 EDT), next fire is 13:00 UTC (= 09:00 EDT) the
        // same day.
        let mut d = def("0 9 * * *");
        d.time_zone = "America/New_York".to_string();
        let after = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 0).unwrap();
        let next = compute_next_fire(&d, after).expect("next");
        assert_eq!(next, Utc.with_ymd_and_hms(2026, 5, 4, 13, 0, 0).unwrap());
    }

    #[test]
    fn compute_next_fire_rejects_unknown_flavor() {
        let mut d = def("*/5 * * * *");
        d.cron_flavor = "garbage".to_string();
        let err = compute_next_fire(&d, Utc::now()).expect_err("must reject");
        assert!(
            matches!(err, SchedulerError::UnknownFlavor { .. }),
            "{err:?}"
        );
    }

    #[test]
    fn compute_next_fire_rejects_invalid_time_zone() {
        let mut d = def("*/5 * * * *");
        d.time_zone = "Mars/Olympus".to_string();
        let err = compute_next_fire(&d, Utc::now()).expect_err("must reject");
        assert!(
            matches!(err, SchedulerError::InvalidTimeZone { .. }),
            "{err:?}"
        );
    }

    #[test]
    fn compute_next_fire_rejects_invalid_cron_expr() {
        let d = def("not a cron expression");
        let err = compute_next_fire(&d, Utc::now()).expect_err("must reject");
        assert!(matches!(err, SchedulerError::InvalidCron { .. }), "{err:?}");
    }

    #[test]
    fn build_lineage_run_id_is_deterministic_per_scheduled_for() {
        let when = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 0).unwrap();
        let a = build_lineage("nightly-rollup", when);
        let b = build_lineage("nightly-rollup", when);
        assert_eq!(a.run_id, b.run_id);
        assert_eq!(a.namespace, SCHEDULER_LINEAGE_NAMESPACE);
        assert_eq!(a.job_name, "nightly-rollup");
        assert_eq!(a.event_time, when);

        // Different scheduled_for ⇒ different run_id.
        let later = when + chrono::Duration::minutes(5);
        let c = build_lineage("nightly-rollup", later);
        assert_ne!(a.run_id, c.run_id);
    }

    #[test]
    fn lineage_constants_are_stable_wire_format() {
        // These show up on the wire; locking them in.
        assert_eq!(SCHEDULER_LINEAGE_NAMESPACE, "of://schedules");
        assert!(SCHEDULER_LINEAGE_PRODUCER.starts_with("https://"));
    }
}
