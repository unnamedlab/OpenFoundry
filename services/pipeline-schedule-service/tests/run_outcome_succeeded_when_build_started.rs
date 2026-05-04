//! Dispatcher → SUCCEEDED outcome when pipeline-build-service accepts
//! the build and returns a build_id.

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::Arc;

use pipeline_schedule_service::domain::build_client::BuildAttemptOutcome;
use pipeline_schedule_service::domain::dispatcher::{
    DispatchTrigger, Dispatcher, DispatcherConfig,
};
use pipeline_schedule_service::domain::run_store::{self, ListRunsFilter, RunOutcome};
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn dispatch_marks_run_succeeded_on_started_build() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "ok").await;

    let build = Arc::new(common::StubBuildClient::always(
        BuildAttemptOutcome::Started {
            build_rid: "ri.foundry.main.build.42".into(),
        },
    ));
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build.clone(),
        notify,
        DispatcherConfig::default(),
    );

    let report = dispatcher
        .dispatch(
            &schedule,
            DispatchTrigger::Manual {
                requested_by: Uuid::now_v7(),
            },
        )
        .await
        .expect("dispatch");
    let run = report.run.expect("run row inserted");
    assert_eq!(run.outcome, RunOutcome::Succeeded);
    assert_eq!(run.build_rid.as_deref(), Some("ri.foundry.main.build.42"));
    assert_eq!(run.failure_reason, None);

    // Build-service got a SCHEDULED-trigger payload.
    let calls = build.recorded_calls();
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0].trigger_kind.as_deref(), Some("SCHEDULED"));

    // The schedule row's `active_run_id` is populated until the
    // downstream build finishes — SUCCEEDED runs stay open.
    let updated = run_store::list_for_schedule(
        &pool,
        schedule.id,
        ListRunsFilter {
            outcome: Some(RunOutcome::Succeeded),
            ..Default::default()
        },
    )
    .await
    .unwrap();
    assert_eq!(updated.len(), 1);
}
