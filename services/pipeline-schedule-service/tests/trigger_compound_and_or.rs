//! End-to-end trigger evaluator behaviour for nested AND / OR
//! Compound triggers. Pure-logic — no Postgres, no HTTP — driven
//! through the `compound_satisfied_event_only` helper exposed by
//! [`pipeline_schedule_service::domain::trigger_engine`].

use pipeline_schedule_service::domain::trigger::{
    CompoundOp, CompoundTrigger, CronFlavor as TriggerCronFlavor, EventTrigger, EventType,
    TimeTrigger, Trigger, TriggerKind,
};
use pipeline_schedule_service::domain::trigger_engine::next_fire_for_schedule;

fn ev(t: EventType, rid: &str) -> Trigger {
    Trigger {
        kind: TriggerKind::Event(EventTrigger {
            event_type: t,
            target_rid: rid.to_string(),
            branch_filter: vec![],
        }),
    }
}

fn time(cron: &str) -> Trigger {
    Trigger {
        kind: TriggerKind::Time(TimeTrigger {
            cron: cron.to_string(),
            time_zone: "UTC".to_string(),
            flavor: TriggerCronFlavor::Unix5,
        }),
    }
}

fn and_(components: Vec<Trigger>) -> Trigger {
    Trigger {
        kind: TriggerKind::Compound(CompoundTrigger {
            op: CompoundOp::And,
            components,
        }),
    }
}

fn or_(components: Vec<Trigger>) -> Trigger {
    Trigger {
        kind: TriggerKind::Compound(CompoundTrigger {
            op: CompoundOp::Or,
            components,
        }),
    }
}

#[test]
fn nested_compound_walks_every_leaf_with_correct_path() {
    // AND( T1, OR(E1, AND(T2, E2)) )
    let trig = and_(vec![
        time("0 9 * * *"),
        or_(vec![
            ev(EventType::DataUpdated, "ri.x"),
            and_(vec![
                time("0 12 * * *"),
                ev(EventType::JobSucceeded, "ri.y"),
            ]),
        ]),
    ]);
    let leaves: Vec<_> = trig.walk_leaves().into_iter().map(|(p, _)| p).collect();
    assert_eq!(
        leaves,
        vec![
            "compound[0].time",
            "compound[1].compound[0].event",
            "compound[1].compound[1].compound[0].time",
            "compound[1].compound[1].compound[1].event",
        ]
    );
}

#[test]
fn or_compound_with_two_time_triggers_returns_earliest_fire() {
    use chrono::{TimeZone, Utc};
    let trig = or_(vec![time("0 9 * * *"), time("0 12 * * *")]);
    // Embed in a synthetic Schedule to call next_fire_for_schedule.
    let schedule = pipeline_schedule_service::domain::trigger::Schedule {
        id: uuid::Uuid::nil(),
        rid: "ri.foundry.main.schedule.t".into(),
        project_rid: "ri.foundry.main.project.t".into(),
        name: "t".into(),
        description: "".into(),
        trigger: trig,
        target: pipeline_schedule_service::domain::trigger::ScheduleTarget {
            kind: pipeline_schedule_service::domain::trigger::ScheduleTargetKind::SyncRun(
                pipeline_schedule_service::domain::trigger::SyncRunTarget {
                    sync_rid: "x".into(),
                    source_rid: "y".into(),
                },
            ),
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
    };
    let after = Utc.with_ymd_and_hms(2026, 4, 27, 10, 30, 0).unwrap();
    let v = next_fire_for_schedule(&schedule, after).unwrap().unwrap();
    assert_eq!(v, Utc.with_ymd_and_hms(2026, 4, 27, 12, 0, 0).unwrap());
}

#[test]
fn and_compound_with_two_time_triggers_returns_max_per_doc_warning() {
    use chrono::{TimeZone, Utc};
    // Per Foundry doc: "AND of multiple time triggers will only be
    // satisfied when all time triggers coincide" — we conservatively
    // surface the latest of the candidate fires.
    let trig = and_(vec![time("0 9 * * *"), time("0 12 * * *")]);
    let schedule = pipeline_schedule_service::domain::trigger::Schedule {
        id: uuid::Uuid::nil(),
        rid: "ri.foundry.main.schedule.t".into(),
        project_rid: "ri.foundry.main.project.t".into(),
        name: "t".into(),
        description: "".into(),
        trigger: trig,
        target: pipeline_schedule_service::domain::trigger::ScheduleTarget {
            kind: pipeline_schedule_service::domain::trigger::ScheduleTargetKind::SyncRun(
                pipeline_schedule_service::domain::trigger::SyncRunTarget {
                    sync_rid: "x".into(),
                    source_rid: "y".into(),
                },
            ),
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
    };
    let after = Utc.with_ymd_and_hms(2026, 4, 27, 0, 0, 0).unwrap();
    let v = next_fire_for_schedule(&schedule, after).unwrap().unwrap();
    // Latest of (9:00, 12:00) → 12:00.
    assert_eq!(v, Utc.with_ymd_and_hms(2026, 4, 27, 12, 0, 0).unwrap());
}

#[test]
fn round_trip_compound_through_json() {
    let trig = or_(vec![
        and_(vec![
            ev(EventType::DataUpdated, "ri.x"),
            ev(EventType::NewLogic, "ri.x"),
        ]),
        time("0 9 * * 1"),
    ]);
    let json = serde_json::to_value(&trig).unwrap();
    let back: Trigger = serde_json::from_value(json).unwrap();
    assert_eq!(trig, back);
}
