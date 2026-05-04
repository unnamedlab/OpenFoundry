//! `auto_pause_exempt = true` opts a schedule out of the auto-pause
//! supervisor entirely. Manual pause still works (covered elsewhere);
//! this test verifies that, even with N consecutive failures, the
//! supervisor leaves the row alone.

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::Arc;

use pipeline_schedule_service::domain::build_client::BuildAttemptOutcome;
use pipeline_schedule_service::domain::dispatcher::{
    AutoPauseConfig, DispatchTrigger, Dispatcher, DispatcherConfig,
};
use pipeline_schedule_service::domain::{schedule_store, trigger::Schedule};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn auto_pause_skipped_when_schedule_is_exempt() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let mut schedule = common::create_pipeline_build_schedule(&pool, "exempt").await;

    // Mark the schedule as exempt up-front.
    schedule = schedule_store::set_auto_pause_exempt(&pool, &schedule.rid, true)
        .await
        .unwrap();

    let build = Arc::new(common::StubBuildClient::always(
        BuildAttemptOutcome::RejectedByService {
            status: 500,
            reason: "kaboom".into(),
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
                consecutive_failures_threshold: 2,
            },
            schedule_link_template: "/build-schedules/{rid}".into(),
        },
    );

    // Drive enough failures to clear the threshold.
    for _ in 0..3 {
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
        assert!(!report.auto_paused);
    }

    let final_state: Schedule = schedule_store::get_by_rid(&pool, &schedule.rid)
        .await
        .unwrap();
    assert!(
        !final_state.paused,
        "exempt schedule must not be auto-paused"
    );
    assert_eq!(
        notify.alert_count(),
        0,
        "no alert should fire for exempt schedules"
    );
}
