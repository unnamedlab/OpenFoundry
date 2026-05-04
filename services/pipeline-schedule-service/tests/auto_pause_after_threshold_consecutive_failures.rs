//! Per the Foundry doc § "Automatically paused schedules": after `N`
//! consecutive failures, the supervisor pauses the schedule and
//! tags the row with `paused_reason='AUTO_PAUSED_AFTER_FAILURES'`.

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
async fn auto_pause_fires_after_threshold_consecutive_failures() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let mut schedule = common::create_pipeline_build_schedule(&pool, "auto-pause-3").await;

    let build = Arc::new(common::StubBuildClient::always(
        BuildAttemptOutcome::RejectedByService {
            status: 500,
            reason: "boom".into(),
        },
    ));
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build,
        notify.clone(),
        DispatcherConfig {
            auto_pause: AutoPauseConfig {
                enabled: true,
                consecutive_failures_threshold: 3,
            },
            schedule_link_template: "/build-schedules/{rid}".into(),
        },
    );

    // Three failed dispatches in a row.
    for _ in 0..3 {
        // Re-fetch so the dispatcher sees the latest active_run_id /
        // paused state (the row mutates as runs land).
        schedule = schedule_store::get_by_rid(&pool, &schedule.rid)
            .await
            .unwrap();
        let report = dispatcher
            .dispatch(
                &schedule,
                DispatchTrigger::Cron {
                    fired_at: chrono::Utc::now(),
                },
            )
            .await
            .unwrap();
        assert!(report.run.is_some());
    }

    let paused: Schedule = schedule_store::get_by_rid(&pool, &schedule.rid)
        .await
        .unwrap();
    assert!(
        paused.paused,
        "schedule should be auto-paused after 3 fails"
    );
    assert_eq!(paused.paused_reason.as_deref(), Some(AUTO_PAUSED_REASON));

    // The notification client got a single auto-paused alert.
    assert_eq!(notify.alert_count(), 1);
    let alert = notify.last_alert().expect("alert");
    assert_eq!(alert.schedule_rid, paused.rid);
    assert_eq!(alert.failed_run_rids.len(), 3);
}
