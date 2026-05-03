//! SCH-001 — idle schedule (no runs in last 90 days). Pure-logic
//! integration test; just exercises the rule against a synthetic
//! inventory.

use chrono::{Duration, TimeZone, Utc};
use scheduling_linter::model::{
    InventoryRun, InventorySchedule, InventoryTrigger, InventoryUser, RuleId, SweepInput,
    TriggerCronFlavor,
};
use scheduling_linter::run_sweep;
use uuid::Uuid;

fn schedule_with_last_run(days_ago: i64, name: &str) -> InventorySchedule {
    let now = Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
    InventorySchedule {
        id: Uuid::now_v7(),
        rid: format!("ri.foundry.main.schedule.{name}"),
        project_rid: "ri.foundry.main.project.t".into(),
        name: name.into(),
        paused: false,
        paused_at: None,
        scope_kind: "USER".into(),
        run_as_user: Some(InventoryUser {
            id: Uuid::now_v7(),
            display_name: "alice".into(),
            active: true,
            last_login_at: Some(now - Duration::days(1)),
        }),
        trigger: InventoryTrigger::Time {
            cron: "0 9 * * *".into(),
            time_zone: "UTC".into(),
            flavor: TriggerCronFlavor::Unix5,
        },
        recent_runs: vec![InventoryRun {
            triggered_at: now - Duration::days(days_ago),
            outcome: "SUCCEEDED".into(),
        }],
    }
}

#[test]
fn sch001_does_not_flag_schedule_with_run_in_last_90_days() {
    let now = Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
    let s = schedule_with_last_run(45, "fresh");
    let report = run_sweep(&SweepInput {
        schedules: vec![s],
        now,
        production: false,
    });
    assert!(report
        .findings
        .iter()
        .all(|f| f.rule_id != RuleId::Sch001InactiveLastNinety));
}

#[test]
fn sch001_flags_schedule_with_only_old_runs() {
    let now = Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
    let s = schedule_with_last_run(120, "stale");
    let report = run_sweep(&SweepInput {
        schedules: vec![s],
        now,
        production: false,
    });
    let count = report
        .findings
        .iter()
        .filter(|f| f.rule_id == RuleId::Sch001InactiveLastNinety)
        .count();
    assert_eq!(count, 1);
}

#[test]
fn sch001_flags_schedule_that_has_never_run() {
    let now = Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
    let mut s = schedule_with_last_run(45, "never");
    s.recent_runs.clear();
    let report = run_sweep(&SweepInput {
        schedules: vec![s],
        now,
        production: false,
    });
    let count = report
        .findings
        .iter()
        .filter(|f| f.rule_id == RuleId::Sch001InactiveLastNinety)
        .count();
    assert_eq!(count, 1);
}
