//! Per the Foundry doc:
//!
//!   "If a schedule is triggered while the previous run is still in
//!    action, then it will remain triggered and run only after the
//!    previous schedule is finished."
//!
//! The dispatcher implements that as `pending_re_run = true` plus an
//! `active_run_id` flag — when a second dispatch lands while a run
//! is still in flight, the second one becomes a no-op except for
//! flipping `pending_re_run`. The run-finished hook then dequeues it.

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::Arc;

use pipeline_schedule_service::domain::build_client::BuildAttemptOutcome;
use pipeline_schedule_service::domain::dispatcher::{
    Dispatcher, DispatcherConfig, DispatchTrigger,
};
use pipeline_schedule_service::domain::{schedule_store, trigger::Schedule};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn second_dispatch_is_coalesced_into_pending_re_run() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let mut schedule = common::create_pipeline_build_schedule(&pool, "coalesce").await;

    let build = Arc::new(common::StubBuildClient::scripted(vec![
        BuildAttemptOutcome::Started {
            build_rid: "ri.foundry.main.build.first".into(),
        },
        BuildAttemptOutcome::Started {
            build_rid: "ri.foundry.main.build.second".into(),
        },
    ]));
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build.clone(),
        notify,
        DispatcherConfig::default(),
    );

    // First dispatch: SUCCEEDED → keeps active_run_id populated.
    let first = dispatcher
        .dispatch(
            &schedule,
            DispatchTrigger::Cron {
                fired_at: chrono::Utc::now(),
            },
        )
        .await
        .unwrap();
    let first_run = first.run.expect("first run row");
    schedule = schedule_store::get_by_rid(&pool, &schedule.rid).await.unwrap();
    assert!(schedule.active_run_id.is_some());

    // Second dispatch while the first is still active.
    let second = dispatcher
        .dispatch(
            &schedule,
            DispatchTrigger::Cron {
                fired_at: chrono::Utc::now(),
            },
        )
        .await
        .unwrap();
    assert!(second.coalesced, "second dispatch must coalesce");
    assert!(second.run.is_none(), "no run row inserted for the coalesced trigger");

    // pending_re_run flag is set, build-service was NOT called twice.
    let mid: Schedule = schedule_store::get_by_rid(&pool, &schedule.rid).await.unwrap();
    assert!(mid.pending_re_run);
    assert_eq!(build.recorded_calls().len(), 1);

    // Finish the first run — the hook flushes the pending re-run,
    // which drives a fresh build-service call.
    dispatcher
        .on_run_finished(schedule.id, first_run.id)
        .await
        .unwrap();
    let after: Schedule = schedule_store::get_by_rid(&pool, &schedule.rid).await.unwrap();
    assert!(!after.pending_re_run, "pending flag cleared after flush");
    assert_eq!(build.recorded_calls().len(), 2, "second build-service call drove by flush");
}
