//! Integration test for the Foundry-doc invariant:
//!
//!   "An event trigger remains satisfied after the event has occurred
//!    until the entire trigger is satisfied and the schedule is run."
//!
//! Booted against an ephemeral Postgres container so we exercise the
//! actual `schedule_event_observations` DDL and trigger evaluator. The
//! test is gated behind the `it-postgres` feature so the default
//! `cargo test` run does not require Docker.

#![cfg(feature = "it-postgres")]

use chrono::Utc;
use pipeline_schedule_service::domain::schedule_store::{self, CreateSchedule};
use pipeline_schedule_service::domain::trigger::{
    CompoundOp, CompoundTrigger, EventTrigger, EventType, ScheduleTarget, ScheduleTargetKind,
    SyncRunTarget, Trigger, TriggerKind,
};
use pipeline_schedule_service::domain::trigger_engine::{
    ObservedEvent, PgTriggerEvaluator, TriggerOutcome,
};
use sqlx::PgPool;
use testing::containers::boot_postgres;

const MIGRATION_SQL: &str = include_str!("../migrations/20260504000080_schedules_init.sql");

async fn apply_schema(pool: &PgPool) {
    // gen_random_uuid() lives in pgcrypto.
    sqlx::query("CREATE EXTENSION IF NOT EXISTS pgcrypto")
        .execute(pool)
        .await
        .expect("pgcrypto");
    sqlx::raw_sql(MIGRATION_SQL)
        .execute(pool)
        .await
        .expect("schedule schema migration");
}

fn and_two_events() -> Trigger {
    Trigger {
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
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn observation_persists_across_calls_and_clears_on_satisfaction() {
    let (_container, pool, _url) = boot_postgres().await;
    apply_schema(&pool).await;

    let schedule = schedule_store::create(
        &pool,
        CreateSchedule {
            project_rid: "ri.foundry.main.project.t".into(),
            name: "two-events".into(),
            description: "AND of two events".into(),
            trigger: and_two_events(),
            target: ScheduleTarget {
                kind: ScheduleTargetKind::SyncRun(SyncRunTarget {
                    sync_rid: "ri.foundry.main.sync.t".into(),
                    source_rid: "ri.foundry.main.source.t".into(),
                }),
            },
            paused: false,
            created_by: "tester".into(),
            run_as_user_id: None,
        },
    )
    .await
    .expect("create schedule");

    let evaluator = PgTriggerEvaluator::new(pool.clone());

    // First event arrives: trigger accepted but not satisfied.
    let outcome_a = evaluator
        .observe(
            &schedule,
            &ObservedEvent {
                event_type: EventType::DataUpdated,
                target_rid: "ri.foundry.main.dataset.x".into(),
                occurred_at: Utc::now(),
            },
        )
        .await
        .unwrap();
    assert_eq!(outcome_a, TriggerOutcome::AcceptedNotSatisfied);

    // Observation must persist between calls.
    let count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM schedule_event_observations WHERE schedule_id = $1",
    )
    .bind(schedule.id)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(count, 1, "first observation must be retained");

    // Unrelated event ignored — it does not match any leaf.
    let outcome_b = evaluator
        .observe(
            &schedule,
            &ObservedEvent {
                event_type: EventType::DataUpdated,
                target_rid: "ri.foundry.main.dataset.OTHER".into(),
                occurred_at: Utc::now(),
            },
        )
        .await
        .unwrap();
    assert_eq!(outcome_b, TriggerOutcome::Ignored);

    // Second event arrives: AND becomes satisfied → observations cleared.
    let outcome_c = evaluator
        .observe(
            &schedule,
            &ObservedEvent {
                event_type: EventType::JobSucceeded,
                target_rid: "ri.foundry.main.job.y".into(),
                occurred_at: Utc::now(),
            },
        )
        .await
        .unwrap();
    assert_eq!(outcome_c, TriggerOutcome::Satisfied);

    let after_count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM schedule_event_observations WHERE schedule_id = $1",
    )
    .bind(schedule.id)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(
        after_count, 0,
        "observations must be cleared when the trigger fires"
    );
}
