//! The P1 BEFORE-UPDATE trigger snapshots the previous version row in
//! `schedule_versions` whenever name / description / trigger / target
//! change. This test exercises that hook through `schedule_store::update`
//! and verifies the snapshot history that backs `GET /v1/schedules/{rid}/versions`.

#![cfg(feature = "it-postgres")]

mod common;

use pipeline_schedule_service::domain::trigger::{
    CronFlavor as TriggerCronFlavor, TimeTrigger, Trigger, TriggerKind,
};
use pipeline_schedule_service::domain::{schedule_store, version_store};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn editing_schedule_records_a_version_snapshot() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "version-test").await;

    // Edit the trigger to a different cron expression.
    let new_trigger = Trigger {
        kind: TriggerKind::Time(TimeTrigger {
            cron: "0 12 * * *".into(),
            time_zone: "UTC".into(),
            flavor: TriggerCronFlavor::Unix5,
        }),
    };
    let updated = schedule_store::update(
        &pool,
        &schedule.rid,
        schedule_store::UpdateSchedule {
            name: Some("renamed".into()),
            trigger: Some(new_trigger.clone()),
            edited_by: "tester".into(),
            change_comment: "switching to noon UTC".into(),
            ..Default::default()
        },
    )
    .await
    .unwrap();
    assert_eq!(updated.version, 2, "version must increment on edit");

    // Versions list returns at least one snapshot — the previous row.
    let versions = version_store::list_versions(&pool, schedule.id, 50, 0)
        .await
        .unwrap();
    assert_eq!(versions.len(), 1);
    let snap = &versions[0];
    assert_eq!(snap.version, 1, "snapshot stores the prior version number");
    assert_eq!(snap.name, "version-test");
    assert_eq!(snap.comment, "switching to noon UTC");

    // The old trigger_json/target_json are pinned in the snapshot row.
    let old_cron = snap
        .trigger_json
        .pointer("/kind/time/cron")
        .and_then(|v| v.as_str());
    assert_eq!(old_cron, Some("0 9 * * *"));
}
