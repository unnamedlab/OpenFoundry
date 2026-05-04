//! Cron dispatch routing for `pipeline-schedule-service` (Tarea 3.5).
//!
//! Determines, for a given persisted [`Schedule`], whether the trigger
//! reduces to one or more cron clauses that the
//! [`event_scheduler`](::event_scheduler) `schedules-tick` CronJob can
//! evaluate, or whether it depends on event observations and must be
//! left to the in-process trigger engine.
//!
//! Pre-3.5 this module also created Temporal Schedules. The Temporal
//! dispatch path has been replaced by writing rows in
//! `schedules.definitions` (Postgres) which the K8s `CronJob` reads
//! every minute — see [`crate::domain::cron_registrar`] for the writer
//! side and the helm template
//! `infra/helm/apps/of-data-engine/templates/cronjob-pipeline-scheduler.yaml`
//! for the runner side.

use crate::domain::trigger::{Schedule as ScheduleRow, Trigger, TriggerKind};

/// One cron clause in a [`CronEmitterPlan::CronEmitter`] plan.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CronClause {
    /// Raw cron expression, parsed with `scheduling-cron::parse_cron`
    /// at fire time by the `schedules-tick` runner.
    pub cron: String,
    /// IANA time zone, e.g. `"Europe/Madrid"`.
    pub time_zone: String,
    /// Cron flavor — `"unix5"` (5-field) or `"quartz6"`
    /// (6-field, seconds-prefixed). Stored as the wire string so it
    /// round-trips through `schedules.definitions.cron_flavor`.
    pub flavor: String,
}

/// Routing decision for a schedule. The handlers use this to pick
/// between writing to `schedules.definitions` (cron-emitter, owned by
/// the CronJob runner) and leaving the schedule to the in-process
/// trigger engine for ad-hoc dispatch.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CronEmitterPlan {
    /// Pure time trigger (or OR-of-time triggers): persist one row per
    /// cron clause in `schedules.definitions`. The `schedules-tick`
    /// pod owns the dispatch from there on.
    CronEmitter { clauses: Vec<CronClause> },
    /// Event / Compound (with at least one non-time component) — the
    /// trigger evaluator enqueues an ad-hoc Kafka event when the
    /// trigger is fully satisfied, so no rows are registered with the
    /// emitter.
    AdHocOnEvent,
}

/// Inspect a persisted schedule and decide how it should be
/// dispatched. Pure helper — no IO.
pub fn dispatch_plan_for(schedule: &ScheduleRow) -> CronEmitterPlan {
    plan_for_trigger(&schedule.trigger)
}

fn flavor_str(flavor: crate::domain::trigger::CronFlavor) -> &'static str {
    match flavor {
        crate::domain::trigger::CronFlavor::Unix5 => "unix5",
        crate::domain::trigger::CronFlavor::Quartz6 => "quartz6",
    }
}

fn plan_for_trigger(trigger: &Trigger) -> CronEmitterPlan {
    match &trigger.kind {
        TriggerKind::Time(t) => CronEmitterPlan::CronEmitter {
            clauses: vec![CronClause {
                cron: t.cron.clone(),
                time_zone: t.time_zone.clone(),
                flavor: flavor_str(t.flavor).to_string(),
            }],
        },
        TriggerKind::Event(_) => CronEmitterPlan::AdHocOnEvent,
        TriggerKind::Compound(c) => {
            // A compound that is *all* time triggers under OR can be
            // expressed as multiple cron clauses on one schedule;
            // everything else falls through to ad-hoc.
            let all_time = c
                .components
                .iter()
                .all(|child| matches!(child.kind, TriggerKind::Time(_)));
            let is_or = matches!(c.op, crate::domain::trigger::CompoundOp::Or);
            if all_time && is_or {
                let clauses = c
                    .components
                    .iter()
                    .filter_map(|child| match &child.kind {
                        TriggerKind::Time(t) => Some(CronClause {
                            cron: t.cron.clone(),
                            time_zone: t.time_zone.clone(),
                            flavor: flavor_str(t.flavor).to_string(),
                        }),
                        _ => None,
                    })
                    .collect();
                CronEmitterPlan::CronEmitter { clauses }
            } else {
                CronEmitterPlan::AdHocOnEvent
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::trigger::{
        CompoundOp, CompoundTrigger, CronFlavor as TriggerCronFlavor, EventTrigger, EventType,
        ScheduleTarget, ScheduleTargetKind, SyncRunTarget, TimeTrigger, Trigger, TriggerKind,
    };
    use uuid::Uuid;

    fn schedule_with(trigger: Trigger) -> ScheduleRow {
        ScheduleRow {
            id: Uuid::nil(),
            rid: "ri.foundry.main.schedule.t".into(),
            project_rid: "ri.foundry.main.project.t".into(),
            name: "t".into(),
            description: "".into(),
            trigger,
            target: ScheduleTarget {
                kind: ScheduleTargetKind::SyncRun(SyncRunTarget {
                    sync_rid: "x".into(),
                    source_rid: "y".into(),
                }),
            },
            paused: false,
            version: 1,
            created_by: "test".into(),
            created_at: chrono::Utc::now(),
            updated_at: chrono::Utc::now(),
            last_run_at: None,
            paused_reason: None,
            paused_at: None,
            auto_pause_exempt: false,
            pending_re_run: false,
            active_run_id: None,
            scope_kind: crate::domain::trigger::ScheduleScopeKind::User,
            project_scope_rids: vec![],
            run_as_user_id: None,
            service_principal_id: None,
        }
    }

    #[test]
    fn pure_time_trigger_maps_to_cron_emitter() {
        let s = schedule_with(Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: "0 9 * * *".into(),
                time_zone: "UTC".into(),
                flavor: TriggerCronFlavor::Unix5,
            }),
        });
        match dispatch_plan_for(&s) {
            CronEmitterPlan::CronEmitter { clauses } => {
                assert_eq!(clauses.len(), 1);
                assert_eq!(clauses[0].cron, "0 9 * * *");
                assert_eq!(clauses[0].time_zone, "UTC");
                assert_eq!(clauses[0].flavor, "unix5");
            }
            other => panic!("expected CronEmitter, got {other:?}"),
        }
    }

    #[test]
    fn pure_event_trigger_maps_to_ad_hoc() {
        let s = schedule_with(Trigger {
            kind: TriggerKind::Event(EventTrigger {
                event_type: EventType::DataUpdated,
                target_rid: "ri.x".into(),
                branch_filter: vec![],
            }),
        });
        assert_eq!(dispatch_plan_for(&s), CronEmitterPlan::AdHocOnEvent);
    }

    #[test]
    fn or_of_time_triggers_collapses_to_multi_clause_emitter() {
        let s = schedule_with(Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::Or,
                components: vec![
                    Trigger {
                        kind: TriggerKind::Time(TimeTrigger {
                            cron: "0 9 * * *".into(),
                            time_zone: "UTC".into(),
                            flavor: TriggerCronFlavor::Unix5,
                        }),
                    },
                    Trigger {
                        kind: TriggerKind::Time(TimeTrigger {
                            cron: "0 17 * * *".into(),
                            time_zone: "Europe/Madrid".into(),
                            flavor: TriggerCronFlavor::Quartz6,
                        }),
                    },
                ],
            }),
        });
        match dispatch_plan_for(&s) {
            CronEmitterPlan::CronEmitter { clauses } => {
                assert_eq!(clauses.len(), 2);
                assert_eq!(clauses[0].cron, "0 9 * * *");
                assert_eq!(clauses[1].cron, "0 17 * * *");
                assert_eq!(clauses[1].time_zone, "Europe/Madrid");
                assert_eq!(clauses[1].flavor, "quartz6");
            }
            other => panic!("expected CronEmitter, got {other:?}"),
        }
    }

    #[test]
    fn and_with_event_falls_back_to_ad_hoc() {
        let s = schedule_with(Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::And,
                components: vec![
                    Trigger {
                        kind: TriggerKind::Time(TimeTrigger {
                            cron: "0 9 * * *".into(),
                            time_zone: "UTC".into(),
                            flavor: TriggerCronFlavor::Unix5,
                        }),
                    },
                    Trigger {
                        kind: TriggerKind::Event(EventTrigger {
                            event_type: EventType::DataUpdated,
                            target_rid: "ri.x".into(),
                            branch_filter: vec![],
                        }),
                    },
                ],
            }),
        });
        assert_eq!(dispatch_plan_for(&s), CronEmitterPlan::AdHocOnEvent);
    }
}
