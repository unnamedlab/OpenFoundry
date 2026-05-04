//! Dispatcher → FAILED outcome when pipeline-build-service returns 5xx.
//! The failure_reason captures the upstream status + body so the
//! Run history UI can surface the cause without paging the operator.

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::Arc;

use pipeline_schedule_service::domain::build_client::BuildAttemptOutcome;
use pipeline_schedule_service::domain::dispatcher::{
    DispatchTrigger, Dispatcher, DispatcherConfig,
};
use pipeline_schedule_service::domain::run_store::RunOutcome;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn dispatch_marks_run_failed_on_build_service_5xx() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "5xx-fails").await;

    let build = Arc::new(common::StubBuildClient::always(
        BuildAttemptOutcome::RejectedByService {
            status: 503,
            reason: "build-service unavailable".into(),
        },
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
    assert_eq!(run.outcome, RunOutcome::Failed);
    assert_eq!(run.build_rid, None);
    assert!(
        run.failure_reason
            .as_deref()
            .unwrap_or_default()
            .contains("503"),
        "failure_reason should record the upstream status, got {:?}",
        run.failure_reason
    );
    assert!(run.finished_at.is_some());
}
