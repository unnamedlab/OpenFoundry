//! Dispatcher → IGNORED outcome when pipeline-build-service signals
//! "all outputs fresh" (D1.1.5 P2 staleness). The schedule_run row's
//! failure_reason carries the staleness verbatim per the doc:
//!
//!   "An ignored run likely indicates that everything is up-to-date and
//!    there was no work to do."

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::Arc;

use pipeline_schedule_service::domain::build_client::BuildAttemptOutcome;
use pipeline_schedule_service::domain::dispatcher::{
    DispatchTrigger, Dispatcher, DispatcherConfig,
};
use pipeline_schedule_service::domain::run_store::RunOutcome;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn dispatch_marks_run_ignored_when_outputs_fresh() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "fresh-skip").await;

    let build = Arc::new(common::StubBuildClient::always(
        BuildAttemptOutcome::AllOutputsFresh,
    ));
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(pool.clone(), build, notify, DispatcherConfig::default());

    let report = dispatcher
        .dispatch(
            &schedule,
            DispatchTrigger::Cron {
                fired_at: chrono::Utc::now(),
            },
        )
        .await
        .expect("dispatch");
    let run = report.run.expect("run row");
    assert_eq!(run.outcome, RunOutcome::Ignored);
    assert_eq!(run.build_rid, None);
    assert_eq!(run.failure_reason.as_deref(), Some("all outputs fresh"));
    // IGNORED runs are terminal — finished_at is set right away.
    assert!(run.finished_at.is_some());
}
