//! Shared test harness for the P2 dispatcher integration tests.
//!
//! `boot_with_schedules_schema()` brings up an ephemeral Postgres,
//! installs the P1 + P2 migrations, and returns a connected pool.
//!
//! [`StubBuildClient`] and [`StubNotificationClient`] are in-memory
//! `BuildServiceClient` / `NotificationClient` impls the dispatcher
//! tests use to script outcomes without booting the actual build /
//! notification services.

#![cfg(feature = "it-postgres")]
#![allow(dead_code)] // Shared across many integration tests; only some use each helper.

use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use pipeline_schedule_service::domain::build_client::{
    BuildAttemptOutcome, BuildServiceClient, CreateBuildPayload,
};
use pipeline_schedule_service::domain::notification_client::{AutoPausedAlert, NotificationClient};
use sqlx::PgPool;
use testing::containers::boot_postgres;

const P1_MIGRATION: &str =
    include_str!("../../migrations/20260504000080_schedules_init.sql");
const P2_MIGRATION: &str = include_str!("../../migrations/20260504000081_schedule_runs.sql");
const P3_MIGRATION: &str = include_str!("../../migrations/20260504000082_schedule_scope.sql");

/// Returned container handle is `impl Drop` — keep it alive for the
/// duration of the test or the postgres process is torn down.
pub async fn boot_with_schedules_schema() -> (impl Drop, PgPool) {
    let (container, pool, _url) = boot_postgres().await;
    sqlx::query("CREATE EXTENSION IF NOT EXISTS pgcrypto")
        .execute(&pool)
        .await
        .expect("pgcrypto");
    sqlx::raw_sql(P1_MIGRATION)
        .execute(&pool)
        .await
        .expect("P1 schedules schema");
    sqlx::raw_sql(P2_MIGRATION)
        .execute(&pool)
        .await
        .expect("P2 schedules schema");
    sqlx::raw_sql(P3_MIGRATION)
        .execute(&pool)
        .await
        .expect("P3 schedule_scope schema");
    (container, pool)
}

#[derive(Clone, Default)]
pub struct StubBuildClient {
    /// Outcome script: pop the front for each call. If empty,
    /// returns `RejectedByService { status: 0, reason: "unscripted" }`.
    pub script: Arc<Mutex<Vec<BuildAttemptOutcome>>>,
    pub recorded: Arc<Mutex<Vec<CreateBuildPayload>>>,
}

impl StubBuildClient {
    pub fn always(outcome: BuildAttemptOutcome) -> Self {
        Self {
            script: Arc::new(Mutex::new(vec![outcome.clone(); 64])),
            recorded: Arc::new(Mutex::new(Vec::new())),
        }
    }

    pub fn scripted(outcomes: Vec<BuildAttemptOutcome>) -> Self {
        Self {
            script: Arc::new(Mutex::new(outcomes)),
            recorded: Arc::new(Mutex::new(Vec::new())),
        }
    }

    pub fn recorded_calls(&self) -> Vec<CreateBuildPayload> {
        self.recorded.lock().unwrap().clone()
    }
}

#[async_trait]
impl BuildServiceClient for StubBuildClient {
    async fn create_build(&self, req: &CreateBuildPayload) -> BuildAttemptOutcome {
        self.recorded.lock().unwrap().push(req.clone());
        let mut script = self.script.lock().unwrap();
        if script.is_empty() {
            BuildAttemptOutcome::RejectedByService {
                status: 0,
                reason: "unscripted".into(),
            }
        } else {
            script.remove(0)
        }
    }
}

#[derive(Clone, Default)]
pub struct StubNotificationClient {
    pub alerts: Arc<Mutex<Vec<AutoPausedAlert>>>,
}

impl StubNotificationClient {
    pub fn alert_count(&self) -> usize {
        self.alerts.lock().unwrap().len()
    }

    pub fn last_alert(&self) -> Option<AutoPausedAlert> {
        self.alerts.lock().unwrap().last().cloned()
    }
}

#[async_trait]
impl NotificationClient for StubNotificationClient {
    async fn send_auto_paused(&self, alert: AutoPausedAlert) {
        self.alerts.lock().unwrap().push(alert);
    }
}

// ---- Schedule fixtures -----------------------------------------------------

use pipeline_schedule_service::domain::schedule_store;
use pipeline_schedule_service::domain::trigger::{
    CompoundOp, CompoundTrigger, CronFlavor as TriggerCronFlavor, EventTrigger, EventType,
    PipelineBuildTarget, ScheduleTarget, ScheduleTargetKind, TimeTrigger, Trigger, TriggerKind,
};

pub async fn create_pipeline_build_schedule(
    pool: &PgPool,
    name: &str,
) -> pipeline_schedule_service::domain::trigger::Schedule {
    schedule_store::create(
        pool,
        schedule_store::CreateSchedule {
            project_rid: "ri.foundry.main.project.t".into(),
            name: name.into(),
            description: "fixture".into(),
            trigger: Trigger {
                kind: TriggerKind::Time(TimeTrigger {
                    cron: "0 9 * * *".into(),
                    time_zone: "UTC".into(),
                    flavor: TriggerCronFlavor::Unix5,
                }),
            },
            target: ScheduleTarget {
                kind: ScheduleTargetKind::PipelineBuild(PipelineBuildTarget {
                    pipeline_rid: "ri.foundry.main.pipeline.alpha".into(),
                    build_branch: "master".into(),
                    job_spec_fallback: vec![],
                    force_build: false,
                    abort_policy: None,
                }),
            },
            paused: false,
            created_by: uuid::Uuid::nil().to_string(),
            run_as_user_id: None,
        },
    )
    .await
    .expect("create schedule fixture")
}

pub async fn create_event_compound_schedule(
    pool: &PgPool,
    name: &str,
) -> pipeline_schedule_service::domain::trigger::Schedule {
    schedule_store::create(
        pool,
        schedule_store::CreateSchedule {
            project_rid: "ri.foundry.main.project.t".into(),
            name: name.into(),
            description: "event-fixture".into(),
            trigger: Trigger {
                kind: TriggerKind::Compound(CompoundTrigger {
                    op: CompoundOp::And,
                    components: vec![
                        Trigger {
                            kind: TriggerKind::Event(EventTrigger {
                                event_type: EventType::DataUpdated,
                                target_rid: "ri.foundry.main.dataset.x".into(),
                                branch_filter: vec![],
                            }),
                        },
                        Trigger {
                            kind: TriggerKind::Event(EventTrigger {
                                event_type: EventType::JobSucceeded,
                                target_rid: "ri.foundry.main.job.y".into(),
                                branch_filter: vec![],
                            }),
                        },
                    ],
                }),
            },
            target: ScheduleTarget {
                kind: ScheduleTargetKind::PipelineBuild(PipelineBuildTarget {
                    pipeline_rid: "ri.foundry.main.pipeline.alpha".into(),
                    build_branch: "master".into(),
                    job_spec_fallback: vec![],
                    force_build: false,
                    abort_policy: None,
                }),
            },
            paused: false,
            created_by: uuid::Uuid::nil().to_string(),
            run_as_user_id: None,
        },
    )
    .await
    .expect("create event schedule fixture")
}
