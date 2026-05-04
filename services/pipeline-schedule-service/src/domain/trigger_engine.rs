//! Trigger evaluator. Computes the next firing instant of a
//! [`Schedule`](super::trigger::Schedule) and decides, given an
//! [`Event`] observation, whether the schedule's compound trigger has
//! become satisfied.
//!
//! Per the Foundry trigger reference:
//!
//! * a `TimeTrigger` is satisfied at the cron-matched wall-clock instant;
//! * an `EventTrigger` becomes satisfied when its named event has
//!   occurred and **stays satisfied** until the parent trigger fires;
//! * a `CompoundTrigger::And` requires every child to be satisfied;
//! * a `CompoundTrigger::Or` requires any child to be satisfied.

use std::str::FromStr;

use chrono::{DateTime, Utc};
use chrono_tz::Tz;
use scheduling_cron::{CronFlavor, CronSchedule, next_fire_after, parse_cron};
use sqlx::PgPool;
use uuid::Uuid;

use super::trigger::{
    CompoundOp, EventTrigger, EventType, Schedule, TimeTrigger, Trigger, TriggerKind,
};

#[derive(Debug, thiserror::Error)]
pub enum TriggerError {
    #[error("invalid cron expression: {0}")]
    InvalidCron(String),
    #[error("unknown time zone '{0}'")]
    InvalidTimeZone(String),
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
}

/// Plain observation row kept in sync with `schedule_event_observations`.
#[derive(Debug, Clone)]
pub struct EventObservation {
    pub schedule_id: Uuid,
    pub trigger_path: String,
    pub event_type: EventType,
    pub target_rid: String,
    pub observed_at: DateTime<Utc>,
}

/// External event delivered by the listener and pushed through the
/// engine for re-evaluation.
#[derive(Debug, Clone)]
pub struct ObservedEvent {
    pub event_type: EventType,
    pub target_rid: String,
    pub occurred_at: DateTime<Utc>,
}

/// Decision returned by [`TriggerEvaluator::observe`] after recording
/// an observation against a schedule's triggers.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TriggerOutcome {
    /// The trigger is now fully satisfied — the schedule should be run.
    /// On dispatch the caller MUST clear the persisted observations
    /// (per "remains satisfied … until the entire trigger is satisfied
    /// and the schedule is run").
    Satisfied,
    /// The trigger has accepted the event but is not yet satisfied
    /// (e.g. a Compound::And waiting on another leaf).
    AcceptedNotSatisfied,
    /// The event did not match any leaf of this schedule's trigger.
    Ignored,
}

/// Compute the next firing instant of `schedule` strictly after `after`.
///
/// Returns `Ok(None)` when the trigger is event-only (no time
/// component to predict).
pub fn next_fire_for_schedule(
    schedule: &Schedule,
    after: DateTime<Utc>,
) -> Result<Option<DateTime<Utc>>, TriggerError> {
    next_fire_for_trigger(&schedule.trigger, after)
}

fn next_fire_for_trigger(
    trigger: &Trigger,
    after: DateTime<Utc>,
) -> Result<Option<DateTime<Utc>>, TriggerError> {
    match &trigger.kind {
        TriggerKind::Time(t) => {
            let s = compile_time_trigger(t)?;
            Ok(next_fire_after(&s, after))
        }
        TriggerKind::Event(_) => Ok(None),
        TriggerKind::Compound(c) => {
            let mut child_fires = Vec::new();
            for child in &c.components {
                if let Some(f) = next_fire_for_trigger(child, after)? {
                    child_fires.push(f);
                }
            }
            // Per the Foundry doc: AND of two time triggers fires at
            // their coincidence (here we approximate as max — the
            // doc warns against this construction explicitly). OR
            // takes the earliest match.
            Ok(match c.op {
                CompoundOp::Or => child_fires.into_iter().min(),
                CompoundOp::And => child_fires.into_iter().max(),
            })
        }
    }
}

fn compile_time_trigger(t: &TimeTrigger) -> Result<CronSchedule, TriggerError> {
    let tz = Tz::from_str(&t.time_zone)
        .map_err(|_| TriggerError::InvalidTimeZone(t.time_zone.clone()))?;
    let flavor: CronFlavor = t.flavor.into();
    parse_cron(&t.cron, flavor, tz).map_err(|e| TriggerError::InvalidCron(e.to_string()))
}

/// Stateful evaluator backed by Postgres.
///
/// `pool` reads / writes `schedule_event_observations`. The struct is
/// stateless across calls (every recompute re-reads observations) so
/// it is safe to clone and share between handlers.
pub struct PgTriggerEvaluator {
    pool: PgPool,
}

impl PgTriggerEvaluator {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }

    /// Record an observation for every leaf of `schedule.trigger` that
    /// matches `event`, then re-evaluate the entire trigger tree
    /// against the persisted observation set. If the tree is fully
    /// satisfied, also clear the observations (the trigger is "reset"
    /// when the schedule runs, per the doc).
    pub async fn observe(
        &self,
        schedule: &Schedule,
        event: &ObservedEvent,
    ) -> Result<TriggerOutcome, TriggerError> {
        let mut tx = self.pool.begin().await?;

        let mut accepted_any = false;
        for (path, kind) in schedule.trigger.walk_leaves() {
            if let TriggerKind::Event(ev) = kind {
                if leaf_matches(ev, event) {
                    sqlx::query(
                        r#"INSERT INTO schedule_event_observations
                            (schedule_id, trigger_path, observed_event_type,
                             observed_target_rid, observed_at)
                           VALUES ($1, $2, $3, $4, $5)
                           ON CONFLICT DO NOTHING"#,
                    )
                    .bind(schedule.id)
                    .bind(&path)
                    .bind(format!("{:?}", event.event_type))
                    .bind(&event.target_rid)
                    .bind(event.occurred_at)
                    .execute(&mut *tx)
                    .await?;
                    accepted_any = true;
                }
            }
        }

        if !accepted_any {
            tx.commit().await?;
            return Ok(TriggerOutcome::Ignored);
        }

        let observations = sqlx::query_as::<_, ObservationRow>(
            r#"SELECT schedule_id, trigger_path, observed_event_type,
                      observed_target_rid, observed_at
                 FROM schedule_event_observations
                WHERE schedule_id = $1"#,
        )
        .bind(schedule.id)
        .fetch_all(&mut *tx)
        .await?;
        let satisfied_paths: std::collections::HashSet<String> = observations
            .into_iter()
            .map(|row| row.trigger_path)
            .collect();

        let satisfied = compound_satisfied_event_only(&schedule.trigger, "", &satisfied_paths);
        if satisfied {
            sqlx::query("DELETE FROM schedule_event_observations WHERE schedule_id = $1")
                .bind(schedule.id)
                .execute(&mut *tx)
                .await?;
            tx.commit().await?;
            Ok(TriggerOutcome::Satisfied)
        } else {
            tx.commit().await?;
            Ok(TriggerOutcome::AcceptedNotSatisfied)
        }
    }
}

#[derive(Debug, sqlx::FromRow)]
struct ObservationRow {
    #[allow(dead_code)]
    schedule_id: Uuid,
    trigger_path: String,
    #[allow(dead_code)]
    observed_event_type: String,
    #[allow(dead_code)]
    observed_target_rid: String,
    #[allow(dead_code)]
    observed_at: DateTime<Utc>,
}

fn leaf_matches(leaf: &EventTrigger, event: &ObservedEvent) -> bool {
    leaf.event_type == event.event_type && leaf.target_rid == event.target_rid
}

/// Compute satisfaction of the trigger tree using only event-leaf
/// observations. Time leaves are treated as `false` here — Time
/// triggers are surfaced via `next_fire_for_schedule` instead, and a
/// Compound::And containing a Time leaf can only fire when the time
/// instant arrives.
fn compound_satisfied_event_only(
    trigger: &Trigger,
    prefix: &str,
    paths: &std::collections::HashSet<String>,
) -> bool {
    match &trigger.kind {
        TriggerKind::Time(_) => false,
        TriggerKind::Event(_) => paths.contains(&join_path(prefix, "event")),
        TriggerKind::Compound(c) => {
            let mut child_results = Vec::with_capacity(c.components.len());
            for (idx, child) in c.components.iter().enumerate() {
                let segment = format!("compound[{idx}]");
                let next_prefix = if prefix.is_empty() {
                    segment
                } else {
                    format!("{prefix}.{segment}")
                };
                child_results.push(compound_satisfied_event_only(child, &next_prefix, paths));
            }
            match c.op {
                CompoundOp::And => child_results.iter().all(|r| *r),
                CompoundOp::Or => child_results.iter().any(|r| *r),
            }
        }
    }
}

fn join_path(prefix: &str, leaf: &str) -> String {
    if prefix.is_empty() {
        leaf.to_string()
    } else {
        format!("{prefix}.{leaf}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::trigger::{
        CompoundTrigger, EventTrigger, EventType, ScheduleTarget, ScheduleTargetKind,
        SyncRunTarget, TimeTrigger,
    };
    use chrono::TimeZone;

    fn schedule_with(trigger: Trigger) -> Schedule {
        Schedule {
            id: Uuid::nil(),
            rid: "ri.foundry.main.schedule.test".into(),
            project_rid: "ri.foundry.main.project.test".into(),
            name: "test".into(),
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
            created_at: Utc::now(),
            updated_at: Utc::now(),
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
    fn next_fire_for_pure_time_trigger() {
        let trig = Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: "30 9 * * 1".into(),
                time_zone: "UTC".into(),
                flavor: super::super::trigger::CronFlavor::Unix5,
            }),
        };
        let s = schedule_with(trig);
        let after = Utc.with_ymd_and_hms(2026, 4, 26, 12, 0, 0).unwrap();
        let v = next_fire_for_schedule(&s, after).unwrap().unwrap();
        assert_eq!(v, Utc.with_ymd_and_hms(2026, 4, 27, 9, 30, 0).unwrap());
    }

    #[test]
    fn next_fire_for_event_only_trigger_returns_none() {
        let trig = Trigger {
            kind: TriggerKind::Event(EventTrigger {
                event_type: EventType::DataUpdated,
                target_rid: "ri.x".into(),
                branch_filter: vec![],
            }),
        };
        let s = schedule_with(trig);
        let v = next_fire_for_schedule(&s, Utc::now()).unwrap();
        assert!(v.is_none());
    }

    #[test]
    fn or_compound_picks_earliest_time_branch() {
        let trig = Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::Or,
                components: vec![
                    Trigger {
                        kind: TriggerKind::Time(TimeTrigger {
                            cron: "0 9 * * *".into(),
                            time_zone: "UTC".into(),
                            flavor: super::super::trigger::CronFlavor::Unix5,
                        }),
                    },
                    Trigger {
                        kind: TriggerKind::Time(TimeTrigger {
                            cron: "0 12 * * *".into(),
                            time_zone: "UTC".into(),
                            flavor: super::super::trigger::CronFlavor::Unix5,
                        }),
                    },
                ],
            }),
        };
        let s = schedule_with(trig);
        let after = Utc.with_ymd_and_hms(2026, 4, 27, 0, 0, 0).unwrap();
        let v = next_fire_for_schedule(&s, after).unwrap().unwrap();
        assert_eq!(v, Utc.with_ymd_and_hms(2026, 4, 27, 9, 0, 0).unwrap());
    }

    #[test]
    fn event_leaf_matches_on_type_and_target() {
        let leaf = EventTrigger {
            event_type: EventType::DataUpdated,
            target_rid: "ri.x".into(),
            branch_filter: vec![],
        };
        let evt = ObservedEvent {
            event_type: EventType::DataUpdated,
            target_rid: "ri.x".into(),
            occurred_at: Utc::now(),
        };
        assert!(leaf_matches(&leaf, &evt));
        let mismatch = ObservedEvent {
            event_type: EventType::JobSucceeded,
            target_rid: "ri.x".into(),
            occurred_at: Utc::now(),
        };
        assert!(!leaf_matches(&leaf, &mismatch));
    }

    #[test]
    fn or_event_compound_satisfied_when_any_observed() {
        let trig = Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::Or,
                components: vec![
                    Trigger {
                        kind: TriggerKind::Event(EventTrigger {
                            event_type: EventType::DataUpdated,
                            target_rid: "ri.x".into(),
                            branch_filter: vec![],
                        }),
                    },
                    Trigger {
                        kind: TriggerKind::Event(EventTrigger {
                            event_type: EventType::JobSucceeded,
                            target_rid: "ri.y".into(),
                            branch_filter: vec![],
                        }),
                    },
                ],
            }),
        };
        let mut paths = std::collections::HashSet::new();
        paths.insert("compound[0].event".to_string());
        assert!(compound_satisfied_event_only(&trig, "", &paths));
    }

    #[test]
    fn and_event_compound_unsatisfied_until_all_observed() {
        let trig = Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::And,
                components: vec![
                    Trigger {
                        kind: TriggerKind::Event(EventTrigger {
                            event_type: EventType::DataUpdated,
                            target_rid: "ri.x".into(),
                            branch_filter: vec![],
                        }),
                    },
                    Trigger {
                        kind: TriggerKind::Event(EventTrigger {
                            event_type: EventType::JobSucceeded,
                            target_rid: "ri.y".into(),
                            branch_filter: vec![],
                        }),
                    },
                ],
            }),
        };
        let mut paths = std::collections::HashSet::new();
        paths.insert("compound[0].event".to_string());
        assert!(!compound_satisfied_event_only(&trig, "", &paths));
        paths.insert("compound[1].event".to_string());
        assert!(compound_satisfied_event_only(&trig, "", &paths));
    }
}
