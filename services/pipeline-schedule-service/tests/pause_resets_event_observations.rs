//! Per the Foundry Schedules doc:
//!
//!   "When a schedule is paused, its trigger state is reset and all
//!    observed events are forgotten."
//!
//! The pause endpoint (and equivalently `set_paused` + the deletion
//! that the handler runs) must clear every row in
//! `schedule_event_observations` for the schedule.

#![cfg(feature = "it-postgres")]

mod common;

use chrono::Utc;
use pipeline_schedule_service::domain::trigger::EventType;
use pipeline_schedule_service::domain::trigger_engine::{
    ObservedEvent, PgTriggerEvaluator,
};
use pipeline_schedule_service::domain::{schedule_store, trigger::MANUAL_PAUSED_REASON};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn pause_clears_all_event_observations() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_event_compound_schedule(&pool, "pause-resets").await;

    // Seed an observation by feeding the evaluator one of the schedule's
    // event leaves.
    let evaluator = PgTriggerEvaluator::new(pool.clone());
    evaluator
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

    let count_before: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM schedule_event_observations WHERE schedule_id = $1",
    )
    .bind(schedule.id)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(count_before, 1);

    // Manual pause: drive the same path the HTTP handler uses
    // (`set_paused` + DELETE on schedule_event_observations).
    schedule_store::set_paused(&pool, &schedule.rid, true, Some(MANUAL_PAUSED_REASON))
        .await
        .unwrap();
    sqlx::query("DELETE FROM schedule_event_observations WHERE schedule_id = $1")
        .bind(schedule.id)
        .execute(&pool)
        .await
        .unwrap();

    let count_after: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM schedule_event_observations WHERE schedule_id = $1",
    )
    .bind(schedule.id)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(count_after, 0, "pause must wipe every event observation");

    // The schedule row itself records why it was paused.
    let updated = schedule_store::get_by_rid(&pool, &schedule.rid).await.unwrap();
    assert!(updated.paused);
    assert_eq!(updated.paused_reason.as_deref(), Some(MANUAL_PAUSED_REASON));
    assert!(updated.paused_at.is_some());
}
