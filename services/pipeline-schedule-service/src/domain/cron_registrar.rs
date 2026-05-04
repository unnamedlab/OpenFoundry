//! `CronRegistrar` — adapter that materialises a [`Schedule`] in
//! `schedules.definitions` so the `schedules-tick` K8s `CronJob`
//! (binary from `libs/event-scheduler`) fires it every minute.
//!
//! Tarea 3.5 — replaces the Temporal `PipelineScheduleClient` adapter
//! that previously owned cron dispatch. The wire contract on the
//! Kafka side is unchanged: the payload mirrors the old
//! `PipelineRunInput` JSON so existing consumers (notably
//! `pipeline-build-service`, Tarea 3.4) continue to deserialize it
//! without code changes.
//!
//! Naming convention: one cron clause = one row, with a deterministic
//! `name` of the form `pipeline-schedule:{rid}:{idx}` so
//! [`CronRegistrar::unregister`] can wipe every row owned by a
//! schedule with a single `LIKE` query without storing the schedule's
//! id on its own column.

use chrono::Utc;
use event_scheduler::{ScheduleDefinition, SchedulerError, compute_next_fire};
use serde_json::json;
use sqlx::PgPool;
use thiserror::Error;
use uuid::Uuid;

use crate::domain::cron_dispatch::CronClause;
use crate::domain::trigger::{Schedule, ScheduleTargetKind};

/// Default Kafka topic the `schedules-tick` runner publishes to for
/// pipeline schedules. Mirrors the topic name `pipeline-build-service`
/// consumes (see Tarea 3.5 statement and
/// `infra/helm/infra/kafka-cluster/values.yaml`).
pub const PIPELINE_SCHEDULED_TOPIC: &str = "pipeline.scheduled.v1";

/// Prefix used by every `schedules.definitions.name` that this
/// registrar owns. Surfaced as a `const` so admin tooling and
/// monitoring queries can scope to it without duplicating the literal.
pub const PIPELINE_SCHEDULE_NAME_PREFIX: &str = "pipeline-schedule:";

#[derive(Debug, Error)]
pub enum RegistrarError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    /// `compute_next_fire` rejected one of the clauses (invalid cron
    /// expression, invalid time zone, unknown flavor, or no future
    /// fire within 10 years). The clause is identified by its
    /// generated `schedules.definitions.name`.
    #[error("invalid cron clause: {0}")]
    InvalidClause(#[from] SchedulerError),
}

/// Postgres-backed registrar — owns the writes/reads against
/// `schedules.definitions`. Cheap to clone (only a `PgPool` handle
/// inside) so the HTTP layer can register it as an Axum `Extension`.
#[derive(Clone)]
pub struct CronRegistrar {
    pg: PgPool,
    topic: String,
}

impl CronRegistrar {
    /// Build a registrar that publishes to [`PIPELINE_SCHEDULED_TOPIC`].
    pub fn new(pg: PgPool) -> Self {
        Self {
            pg,
            topic: PIPELINE_SCHEDULED_TOPIC.to_string(),
        }
    }

    /// Build a registrar that publishes to a custom topic. Useful for
    /// tests and for hypothetical multi-topic deployments.
    pub fn with_topic(pg: PgPool, topic: impl Into<String>) -> Self {
        Self {
            pg,
            topic: topic.into(),
        }
    }

    /// The Kafka topic this registrar writes into every row.
    pub fn topic(&self) -> &str {
        &self.topic
    }

    /// Register one row per cron clause, replacing any rows previously
    /// owned by `schedule.rid`. Idempotent — re-running with the same
    /// schedule + clauses is a no-op apart from advancing
    /// `next_run_at` to the next computed instant.
    ///
    /// `enabled` controls whether the runner picks the rows up; pause
    /// flips this column without dropping the row so resume can flip
    /// it back on.
    pub async fn register(
        &self,
        schedule: &Schedule,
        clauses: &[CronClause],
        enabled: bool,
    ) -> Result<(), RegistrarError> {
        let payload = build_payload(schedule);
        let now = Utc::now();

        let mut tx = self.pg.begin().await?;

        // Wipe any pre-existing rows owned by this schedule so we
        // never leak orphaned clauses when an edit reduces the clause
        // count (e.g. OR-of-time → single-cron).
        sqlx::query("DELETE FROM schedules.definitions WHERE name LIKE $1")
            .bind(format!("{PIPELINE_SCHEDULE_NAME_PREFIX}{}:%", schedule.rid))
            .execute(&mut *tx)
            .await?;

        for (idx, clause) in clauses.iter().enumerate() {
            let name = format!("{PIPELINE_SCHEDULE_NAME_PREFIX}{}:{}", schedule.rid, idx);
            // Pre-validate the cron clause and seed `next_run_at`
            // against `now` so the very first tick after registration
            // either fires immediately (rare — caller passed a cron
            // due at `now`) or, more typically, fires at the next
            // matching instant. The `event_scheduler` runner advances
            // this column itself on every fire.
            let probe = ScheduleDefinition {
                id: Uuid::nil(),
                name: name.clone(),
                cron_expr: clause.cron.clone(),
                cron_flavor: clause.flavor.clone(),
                time_zone: clause.time_zone.clone(),
                enabled,
                topic: self.topic.clone(),
                payload_template: payload.clone(),
                next_run_at: now,
                last_run_at: None,
            };
            let next_run_at = compute_next_fire(&probe, now)?;

            sqlx::query(
                "INSERT INTO schedules.definitions ( \
                    id, name, cron_expr, cron_flavor, time_zone, enabled, \
                    topic, payload_template, next_run_at \
                 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)",
            )
            .bind(Uuid::now_v7())
            .bind(&name)
            .bind(&clause.cron)
            .bind(&clause.flavor)
            .bind(&clause.time_zone)
            .bind(enabled)
            .bind(&self.topic)
            .bind(&payload)
            .bind(next_run_at)
            .execute(&mut *tx)
            .await?;
        }

        tx.commit().await?;
        Ok(())
    }

    /// Drop every row owned by `schedule_rid`. Returns the number of
    /// rows actually removed (0 on a schedule that was never
    /// registered, e.g. event-only triggers).
    pub async fn unregister(&self, schedule_rid: &str) -> Result<u64, RegistrarError> {
        let res = sqlx::query("DELETE FROM schedules.definitions WHERE name LIKE $1")
            .bind(format!("{PIPELINE_SCHEDULE_NAME_PREFIX}{schedule_rid}:%"))
            .execute(&self.pg)
            .await?;
        Ok(res.rows_affected())
    }

    /// Flip `enabled` on every row owned by `schedule_rid`. Used by
    /// pause / resume so the rows keep their `next_run_at` history
    /// across the toggle.
    pub async fn set_enabled(
        &self,
        schedule_rid: &str,
        enabled: bool,
    ) -> Result<u64, RegistrarError> {
        let res = sqlx::query(
            "UPDATE schedules.definitions \
             SET enabled = $2, updated_at = now() \
             WHERE name LIKE $1",
        )
        .bind(format!("{PIPELINE_SCHEDULE_NAME_PREFIX}{schedule_rid}:%"))
        .bind(enabled)
        .execute(&self.pg)
        .await?;
        Ok(res.rows_affected())
    }

    /// Publish a one-shot fire of `schedule` immediately, by inserting
    /// a single disposable row whose `next_run_at = now` and a cron
    /// clause that never matches a real instant — the runner consumes
    /// the row on the very next tick, advancing `next_run_at` past
    /// the 10-year horizon and effectively neutralising the row.
    /// Used by the `:run-now` REST endpoint to reuse the same
    /// emitter pipeline as scheduled fires.
    pub async fn run_now(&self, schedule: &Schedule, run_id: Uuid) -> Result<(), RegistrarError> {
        let payload = build_payload(schedule);
        let one_shot_name =
            format!("{PIPELINE_SCHEDULE_NAME_PREFIX}{}:run-now:{run_id}", schedule.rid);
        // A cron expression that next fires far in the future ensures
        // the row is fired exactly once on the next tick (because
        // `next_run_at` is `now` and `enabled` is true) and then
        // pushed out of the hot scan window forever.
        let cron_far_future = "0 0 1 1 0";
        let now = Utc::now();
        sqlx::query(
            "INSERT INTO schedules.definitions ( \
                id, name, cron_expr, cron_flavor, time_zone, enabled, \
                topic, payload_template, next_run_at \
             ) VALUES ($1,$2,$3,'unix5','UTC',true,$4,$5,$6) \
             ON CONFLICT (name) DO NOTHING",
        )
        .bind(Uuid::now_v7())
        .bind(&one_shot_name)
        .bind(cron_far_future)
        .bind(&self.topic)
        .bind(&payload)
        .bind(now)
        .execute(&self.pg)
        .await?;
        Ok(())
    }
}

/// Build the Kafka payload published on every fire. Mirrors the old
/// `PipelineRunInput` JSON shape so downstream consumers
/// (`pipeline-build-service`, etc.) don't need to special-case the
/// new emitter.
fn build_payload(schedule: &Schedule) -> serde_json::Value {
    let pipeline_rid = match &schedule.target.kind {
        ScheduleTargetKind::PipelineBuild(t) => Some(t.pipeline_rid.clone()),
        ScheduleTargetKind::DatasetBuild(t) => Some(t.dataset_rid.clone()),
        ScheduleTargetKind::SyncRun(_) | ScheduleTargetKind::HealthCheck(_) => None,
    };
    json!({
        "schedule_rid": schedule.rid,
        "pipeline_id": schedule.id,
        "tenant_id": schedule.project_rid,
        "revision": serde_json::Value::Null,
        "parameters": {
            "schedule_rid": schedule.rid,
            "pipeline_rid": pipeline_rid,
        },
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::trigger::{
        DatasetBuildTarget, PipelineBuildTarget, ScheduleScopeKind, ScheduleTarget,
    };

    fn schedule_with_target(target: ScheduleTarget) -> Schedule {
        Schedule {
            id: Uuid::nil(),
            rid: "ri.foundry.main.schedule.x".into(),
            project_rid: "ri.foundry.main.project.x".into(),
            name: "test".into(),
            description: "".into(),
            trigger: crate::domain::trigger::Trigger {
                kind: crate::domain::trigger::TriggerKind::Time(
                    crate::domain::trigger::TimeTrigger {
                        cron: "* * * * *".into(),
                        time_zone: "UTC".into(),
                        flavor: crate::domain::trigger::CronFlavor::Unix5,
                    },
                ),
            },
            target,
            paused: false,
            version: 1,
            created_by: "u".into(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
            last_run_at: None,
            paused_reason: None,
            paused_at: None,
            auto_pause_exempt: false,
            pending_re_run: false,
            active_run_id: None,
            scope_kind: ScheduleScopeKind::User,
            project_scope_rids: vec![],
            run_as_user_id: None,
            service_principal_id: None,
        }
    }

    #[test]
    fn payload_carries_pipeline_rid_for_pipeline_build_target() {
        let s = schedule_with_target(ScheduleTarget {
            kind: ScheduleTargetKind::PipelineBuild(PipelineBuildTarget {
                pipeline_rid: "ri.pipeline.42".into(),
                build_branch: "master".into(),
                job_spec_fallback: vec![],
                force_build: false,
                abort_policy: None,
            }),
        });
        let p = build_payload(&s);
        assert_eq!(p["parameters"]["pipeline_rid"], "ri.pipeline.42");
        assert_eq!(p["schedule_rid"], "ri.foundry.main.schedule.x");
        assert_eq!(p["tenant_id"], "ri.foundry.main.project.x");
    }

    #[test]
    fn payload_uses_dataset_rid_for_dataset_build_target() {
        let s = schedule_with_target(ScheduleTarget {
            kind: ScheduleTargetKind::DatasetBuild(DatasetBuildTarget {
                dataset_rid: "ri.dataset.7".into(),
                build_branch: "master".into(),
                force_build: false,
            }),
        });
        let p = build_payload(&s);
        assert_eq!(p["parameters"]["pipeline_rid"], "ri.dataset.7");
    }

    #[test]
    fn payload_pipeline_rid_is_null_for_sync_target() {
        let s = schedule_with_target(ScheduleTarget {
            kind: ScheduleTargetKind::SyncRun(crate::domain::trigger::SyncRunTarget {
                sync_rid: "ri.sync.1".into(),
                source_rid: "ri.src.1".into(),
            }),
        });
        let p = build_payload(&s);
        assert!(p["parameters"]["pipeline_rid"].is_null());
    }

    #[test]
    fn topic_default_is_pipeline_scheduled_v1() {
        // Constant locked because consumers (pipeline-build-service)
        // subscribe by literal name.
        assert_eq!(PIPELINE_SCHEDULED_TOPIC, "pipeline.scheduled.v1");
    }
}
