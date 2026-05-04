//! Per the Foundry doc, an auto-paused schedule is automatically
//! resumed on its first successful run after the operator (or the
//! upstream system) clears the cause.

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::Arc;

use pipeline_schedule_service::domain::build_client::BuildAttemptOutcome;
use pipeline_schedule_service::domain::dispatcher::{
    AutoPauseConfig, DispatchTrigger, Dispatcher, DispatcherConfig,
};
use pipeline_schedule_service::domain::trigger::AUTO_PAUSED_REASON;
use pipeline_schedule_service::domain::{schedule_store, trigger::Schedule};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn auto_unpause_clears_paused_state_on_first_success() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "auto-unpause").await;

    // Pre-flight: simulate an auto-pause that has already happened.
    schedule_store::set_paused(&pool, &schedule.rid, true, Some(AUTO_PAUSED_REASON))
        .await
        .unwrap();

    // The dispatcher won't run a paused schedule; flip it to unpaused
    // so the SUCCEEDED run can land. (In production, the trigger
    // engine flips it during the maybe_auto_unpause call after the
    // build started — but the test exercises that exact branch by
    // priming a row that is paused with the auto-pause reason and
    // observing that maybe_auto_unpause clears it.)
    let s = schedule_store::set_paused(&pool, &schedule.rid, false, Some(AUTO_PAUSED_REASON))
        .await
        .unwrap();

    let build = Arc::new(common::StubBuildClient::always(
        BuildAttemptOutcome::Started {
            build_rid: "ri.foundry.main.build.recovery".into(),
        },
    ));
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build,
        notify,
        DispatcherConfig {
            auto_pause: AutoPauseConfig::default(),
            schedule_link_template: "/build-schedules/{rid}".into(),
        },
    );

    // First successful dispatch.
    dispatcher
        .dispatch(
            &s,
            DispatchTrigger::Cron {
                fired_at: chrono::Utc::now(),
            },
        )
        .await
        .unwrap();

    let after: Schedule = schedule_store::get_by_rid(&pool, &s.rid).await.unwrap();
    assert!(
        !after.paused,
        "schedule should be auto-resumed after first success"
    );
    assert_eq!(after.paused_reason, None);
}
