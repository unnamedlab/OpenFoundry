//! Temporal Schedules adapter for `pipeline-schedule-service` (S2.4.b).
//!
//! Wraps [`temporal_client::PipelineScheduleClient`] behind a small
//! request DTO so HTTP handlers stay thin. The CRUD shape on the
//! Postgres side (the **declarative** schedule rows) is not touched
//! here — that is still owned by `domain::schedule`. This module
//! takes care of materialising a Postgres row as a live Temporal
//! Schedule and tearing it down.

use serde::{Deserialize, Serialize};
use temporal_client::{PipelineRunInput, PipelineScheduleClient, Result};
use uuid::Uuid;

/// REST payload for `POST /api/v1/data-integration/schedules/temporal`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateTemporalScheduleRequest {
    /// Stable identifier — must match the Postgres row's primary key
    /// so `delete_schedule` is symmetric.
    pub schedule_id: String,
    pub pipeline_id: Uuid,
    pub tenant_id: String,
    #[serde(default)]
    pub revision: Option<String>,
    /// Cron expressions (Temporal syntax = standard 5-field cron with
    /// optional 6th seconds field).
    pub cron_expressions: Vec<String>,
    /// IANA timezone, e.g. `"Europe/Madrid"`. Defaults to UTC.
    #[serde(default)]
    pub timezone: Option<String>,
    /// Optional payload merged into [`PipelineRunInput::parameters`].
    #[serde(default)]
    pub parameters: serde_json::Value,
    /// Inbound audit-correlation ID propagated as Temporal search
    /// attribute (ADR-0019). Auto-generated if absent.
    #[serde(default)]
    pub audit_correlation_id: Option<Uuid>,
}

/// Pure helper: build the typed [`PipelineRunInput`] from a REST
/// request. Unit-testable without Temporal.
pub fn to_run_input(req: &CreateTemporalScheduleRequest) -> PipelineRunInput {
    let parameters = match &req.parameters {
        serde_json::Value::Object(_) => req.parameters.clone(),
        _ => serde_json::Value::Object(serde_json::Map::new()),
    };
    PipelineRunInput {
        pipeline_id: req.pipeline_id,
        tenant_id: req.tenant_id.clone(),
        revision: req.revision.clone(),
        parameters,
    }
}

pub async fn create_schedule(
    client: &PipelineScheduleClient,
    req: &CreateTemporalScheduleRequest,
) -> Result<()> {
    let input = to_run_input(req);
    let audit = req.audit_correlation_id.unwrap_or_else(Uuid::now_v7);
    client
        .create(
            req.schedule_id.clone(),
            req.cron_expressions.clone(),
            req.timezone.clone(),
            input,
            audit,
        )
        .await
}

pub async fn delete_schedule(client: &PipelineScheduleClient, schedule_id: &str) -> Result<()> {
    client.delete(schedule_id).await
}

// ---- Trigger-kind routing --------------------------------------------------
//
// Per the redesign:
//
//   * a pure Time trigger maps cleanly to Temporal's native cron clause
//     (durable, exactly-once, HA failover for free);
//   * an Event or Compound trigger does NOT — the next firing time
//     depends on observation state held in `schedule_event_observations`,
//     which Temporal can't introspect. Those schedules stay in Postgres
//     as the source of truth and the `trigger_engine` enqueues a
//     one-shot Temporal workflow when the trigger becomes satisfied.

use crate::domain::trigger::{Schedule as ScheduleRow, Trigger, TriggerKind};

/// Routing decision for a schedule. The handler/listener uses this to
/// pick between `client.create(...)` (cron clause) and a transient
/// run dispatched by `trigger_engine`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DispatchPlan {
    /// Pure time trigger — register a Temporal Schedule with the given
    /// cron clauses and time zone. Temporal owns the dispatch from
    /// here on.
    TemporalCron {
        cron_expressions: Vec<String>,
        timezone: Option<String>,
    },
    /// Event / Compound — Postgres remains the source of truth and the
    /// trigger evaluator enqueues an ad-hoc workflow run when the
    /// trigger is fully satisfied.
    AdHocOnEvent,
}

pub fn dispatch_plan_for(schedule: &ScheduleRow) -> DispatchPlan {
    plan_for_trigger(&schedule.trigger)
}

fn plan_for_trigger(trigger: &Trigger) -> DispatchPlan {
    match &trigger.kind {
        TriggerKind::Time(t) => DispatchPlan::TemporalCron {
            cron_expressions: vec![t.cron.clone()],
            timezone: Some(t.time_zone.clone()),
        },
        TriggerKind::Event(_) => DispatchPlan::AdHocOnEvent,
        TriggerKind::Compound(c) => {
            // A compound that is *all* time triggers under OR can also
            // be expressed as multiple cron clauses on one Temporal
            // schedule; everything else falls through to ad-hoc.
            let all_time = c
                .components
                .iter()
                .all(|child| matches!(child.kind, TriggerKind::Time(_)));
            let is_or = matches!(c.op, crate::domain::trigger::CompoundOp::Or);
            if all_time && is_or {
                let mut crons = Vec::new();
                let mut tz = None::<String>;
                for child in &c.components {
                    if let TriggerKind::Time(t) = &child.kind {
                        crons.push(t.cron.clone());
                        if tz.is_none() {
                            tz = Some(t.time_zone.clone());
                        }
                    }
                }
                DispatchPlan::TemporalCron {
                    cron_expressions: crons,
                    timezone: tz,
                }
            } else {
                DispatchPlan::AdHocOnEvent
            }
        }
    }
}

#[cfg(test)]
mod plan_tests {
    use super::*;
    use crate::domain::trigger::{
        CompoundOp, CompoundTrigger, CronFlavor as TriggerCronFlavor, EventTrigger, EventType,
        ScheduleTarget, ScheduleTargetKind, SyncRunTarget, TimeTrigger, Trigger, TriggerKind,
    };

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
    fn pure_time_trigger_maps_to_temporal_cron() {
        let s = schedule_with(Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: "0 9 * * *".into(),
                time_zone: "UTC".into(),
                flavor: TriggerCronFlavor::Unix5,
            }),
        });
        match dispatch_plan_for(&s) {
            DispatchPlan::TemporalCron {
                cron_expressions,
                timezone,
            } => {
                assert_eq!(cron_expressions, vec!["0 9 * * *"]);
                assert_eq!(timezone.as_deref(), Some("UTC"));
            }
            other => panic!("expected TemporalCron, got {other:?}"),
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
        assert_eq!(dispatch_plan_for(&s), DispatchPlan::AdHocOnEvent);
    }

    #[test]
    fn or_of_time_triggers_collapses_to_multi_cron_temporal() {
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
                            time_zone: "UTC".into(),
                            flavor: TriggerCronFlavor::Unix5,
                        }),
                    },
                ],
            }),
        });
        match dispatch_plan_for(&s) {
            DispatchPlan::TemporalCron {
                cron_expressions, ..
            } => assert_eq!(cron_expressions, vec!["0 9 * * *", "0 17 * * *"]),
            other => panic!("expected TemporalCron, got {other:?}"),
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
        assert_eq!(dispatch_plan_for(&s), DispatchPlan::AdHocOnEvent);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn run_input_normalises_non_object_parameters() {
        let req = CreateTemporalScheduleRequest {
            schedule_id: "x".into(),
            pipeline_id: Uuid::now_v7(),
            tenant_id: "t".into(),
            revision: None,
            cron_expressions: vec![],
            timezone: None,
            parameters: serde_json::Value::String("not an object".into()),
            audit_correlation_id: None,
        };
        let input = to_run_input(&req);
        assert!(input.parameters.is_object());
    }

    #[test]
    fn run_input_preserves_object_parameters() {
        let req = CreateTemporalScheduleRequest {
            schedule_id: "daily".into(),
            pipeline_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            revision: Some("v3".into()),
            cron_expressions: vec!["0 6 * * *".into()],
            timezone: Some("Europe/Madrid".into()),
            parameters: serde_json::json!({"limit": 1000}),
            audit_correlation_id: None,
        };
        let input = to_run_input(&req);
        assert_eq!(input.tenant_id, "acme");
        assert_eq!(input.revision.as_deref(), Some("v3"));
        assert_eq!(input.parameters["limit"], 1000);
    }
}
