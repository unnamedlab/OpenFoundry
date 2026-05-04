//! Pure-logic verification of the trigger-engine's
//! `next_fire_for_schedule` sequence semantics — exactly what the
//! `GET /v1/schedules/{rid}/preview-next-fires?count=N` endpoint emits.
//!
//! No DB / HTTP: we drive the engine directly so the test is
//! deterministic and order-of-magnitude faster than booting a Postgres
//! testkit just to read back the Schedule row.

use chrono::{DateTime, TimeZone, Utc};
use pipeline_schedule_service::domain::trigger::{
    CronFlavor as TriggerCronFlavor, Schedule, ScheduleTarget, ScheduleTargetKind, SyncRunTarget,
    TimeTrigger, Trigger, TriggerKind,
};
use pipeline_schedule_service::domain::trigger_engine::next_fire_for_schedule;
use uuid::Uuid;

fn schedule_with_cron(cron: &str, tz: &str) -> Schedule {
    Schedule {
        id: Uuid::nil(),
        rid: "ri.foundry.main.schedule.preview".into(),
        project_rid: "ri.foundry.main.project.preview".into(),
        name: "preview".into(),
        description: "".into(),
        trigger: Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: cron.into(),
                time_zone: tz.into(),
                flavor: TriggerCronFlavor::Unix5,
            }),
        },
        target: ScheduleTarget {
            kind: ScheduleTargetKind::SyncRun(SyncRunTarget {
                sync_rid: "x".into(),
                source_rid: "y".into(),
            }),
        },
        paused: false,
        version: 1,
        created_by: "test".into(),
        created_at: Utc::now(),
        updated_at: Utc::now(),
        last_run_at: None,
        paused_reason: None,
        paused_at: None,
        auto_pause_exempt: false,
        pending_re_run: false,
        active_run_id: None,
        scope_kind: pipeline_schedule_service::domain::trigger::ScheduleScopeKind::User,
        project_scope_rids: vec![],
        run_as_user_id: None,
        service_principal_id: None,
    }
}

fn iterate_fires(schedule: &Schedule, after: DateTime<Utc>, count: usize) -> Vec<DateTime<Utc>> {
    let mut cursor = after;
    let mut out = Vec::with_capacity(count);
    for _ in 0..count {
        match next_fire_for_schedule(schedule, cursor).unwrap() {
            Some(next) => {
                out.push(next);
                cursor = next;
            }
            None => break,
        }
    }
    out
}

#[test]
fn preview_for_daily_9am_returns_consecutive_days() {
    let s = schedule_with_cron("0 9 * * *", "UTC");
    let after = Utc.with_ymd_and_hms(2026, 4, 27, 0, 0, 0).unwrap();
    let fires = iterate_fires(&s, after, 5);
    assert_eq!(
        fires,
        vec![
            Utc.with_ymd_and_hms(2026, 4, 27, 9, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 28, 9, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 29, 9, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 30, 9, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 5, 1, 9, 0, 0).unwrap(),
        ]
    );
}

#[test]
fn preview_for_every_15_minutes_returns_5_quarter_hours() {
    let s = schedule_with_cron("*/15 * * * *", "UTC");
    let after = Utc.with_ymd_and_hms(2026, 4, 27, 8, 1, 0).unwrap();
    let fires = iterate_fires(&s, after, 5);
    assert_eq!(
        fires,
        vec![
            Utc.with_ymd_and_hms(2026, 4, 27, 8, 15, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 27, 8, 30, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 27, 8, 45, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 27, 9, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 4, 27, 9, 15, 0).unwrap(),
        ]
    );
}

#[test]
fn preview_in_madrid_tz_emits_utc_offsets_correctly() {
    let s = schedule_with_cron("0 6 * * *", "Europe/Madrid");
    // Mid-summer Madrid = CEST (UTC+2). 06:00 local → 04:00 UTC.
    let after = Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap();
    let fires = iterate_fires(&s, after, 3);
    assert_eq!(
        fires,
        vec![
            Utc.with_ymd_and_hms(2026, 6, 1, 4, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 6, 2, 4, 0, 0).unwrap(),
            Utc.with_ymd_and_hms(2026, 6, 3, 4, 0, 0).unwrap(),
        ]
    );
}

#[test]
fn preview_returns_empty_for_event_only_trigger() {
    use pipeline_schedule_service::domain::trigger::{EventTrigger, EventType};
    let mut s = schedule_with_cron("0 9 * * *", "UTC");
    s.trigger = Trigger {
        kind: TriggerKind::Event(EventTrigger {
            event_type: EventType::DataUpdated,
            target_rid: "ri.x".into(),
            branch_filter: vec![],
        }),
    };
    let after = Utc.with_ymd_and_hms(2026, 4, 27, 0, 0, 0).unwrap();
    assert_eq!(next_fire_for_schedule(&s, after).unwrap(), None);
}
