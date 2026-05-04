//! Troubleshooting analyser.
//!
//! Per Foundry doc § "Troubleshooting reference", a schedule's
//! current health story is a function of:
//!
//!   * its trigger satisfaction state (which Event leaves are still
//!     pending? which cron has not fired? compound parents that are
//!     stuck because a child hasn't observed yet?);
//!   * recent failures from `schedule_runs` (auto-pause symptom);
//!   * configuration smells (paused, stale owner, etc).
//!
//! The analyser inspects these inputs and emits a list of
//! [`SuggestedAction`]s the UI surfaces in the Troubleshooting panel.
//! It is pure-logic — host services materialise the inputs from
//! Postgres and feed them in.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::domain::run_store::{RunOutcome, ScheduleRun};
use crate::domain::trigger::{
    AUTO_PAUSED_REASON, Schedule, Trigger, TriggerKind,
};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct TriggerStateView {
    pub satisfied: bool,
    /// Human-readable description of every leaf still blocking the
    /// trigger (`"Event leaf compound[1].event awaiting DATA_UPDATED on ri…"` etc).
    pub blocking_components: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub enum SuggestedAction {
    /// Schedule is auto-paused. Surface the doc snippet on
    /// auto-pause recovery.
    ResumeAfterAutoPause,
    /// Recent failures cluster on a single error string — operator
    /// should inspect the build-service log.
    InspectRecentFailures { failure_reason: String, count: usize },
    /// The trigger is event-only and has no observed events yet —
    /// the upstream may not be emitting.
    CheckUpstreamEventEmission { target_rid: String },
    /// The trigger is time-only with a high-frequency cron that the
    /// linter would flag.
    ConsiderLowerFrequency { cron: String },
    /// Schedule is paused but for a generic reason; ask whether to
    /// resume.
    ConfirmManualResume,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TroubleshootReport {
    pub schedule_rid: String,
    pub trigger_state: TriggerStateView,
    pub last_failures: Vec<LastFailure>,
    pub suggested_actions: Vec<SuggestedAction>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LastFailure {
    pub run_rid: String,
    pub failure_reason: Option<String>,
    pub triggered_at: DateTime<Utc>,
}

/// Compute the trigger-state view from a [`Schedule`] + an
/// `observed_event_paths` set the caller pulled from
/// `schedule_event_observations`.
pub fn compute_trigger_state(
    schedule: &Schedule,
    observed_event_paths: &std::collections::HashSet<String>,
) -> TriggerStateView {
    let mut blocking = Vec::new();
    let satisfied = walk_trigger(&schedule.trigger, "", observed_event_paths, &mut blocking);
    TriggerStateView {
        satisfied,
        blocking_components: blocking,
    }
}

fn walk_trigger(
    trigger: &Trigger,
    prefix: &str,
    observed: &std::collections::HashSet<String>,
    blocking: &mut Vec<String>,
) -> bool {
    match &trigger.kind {
        TriggerKind::Time(t) => {
            // Time leaves are always "ready to fire" — they're never
            // blocking from a satisfaction standpoint.
            let _ = t;
            true
        }
        TriggerKind::Event(e) => {
            let path = if prefix.is_empty() {
                "event".to_string()
            } else {
                format!("{prefix}.event")
            };
            if observed.contains(&path) {
                true
            } else {
                blocking.push(format!(
                    "Event leaf {path} awaiting {:?} on {}",
                    e.event_type, e.target_rid
                ));
                false
            }
        }
        TriggerKind::Compound(c) => {
            use crate::domain::trigger::CompoundOp;
            let mut child_results = Vec::with_capacity(c.components.len());
            for (idx, child) in c.components.iter().enumerate() {
                let next_prefix = if prefix.is_empty() {
                    format!("compound[{idx}]")
                } else {
                    format!("{prefix}.compound[{idx}]")
                };
                child_results.push(walk_trigger(child, &next_prefix, observed, blocking));
            }
            match c.op {
                CompoundOp::And => child_results.iter().all(|r| *r),
                CompoundOp::Or => child_results.iter().any(|r| *r),
            }
        }
    }
}

/// Compose the full troubleshoot report from inputs the host service
/// materialises from Postgres.
pub fn troubleshoot_schedule(
    schedule: &Schedule,
    observed_event_paths: &std::collections::HashSet<String>,
    recent_runs: &[ScheduleRun],
) -> TroubleshootReport {
    let trigger_state = compute_trigger_state(schedule, observed_event_paths);

    let last_failures: Vec<LastFailure> = recent_runs
        .iter()
        .filter(|r| r.outcome == RunOutcome::Failed)
        .take(5)
        .map(|r| LastFailure {
            run_rid: r.rid.clone(),
            failure_reason: r.failure_reason.clone(),
            triggered_at: r.triggered_at,
        })
        .collect();

    let mut suggested = Vec::new();

    if schedule.paused && schedule.paused_reason.as_deref() == Some(AUTO_PAUSED_REASON) {
        suggested.push(SuggestedAction::ResumeAfterAutoPause);
    } else if schedule.paused {
        suggested.push(SuggestedAction::ConfirmManualResume);
    }

    if last_failures.len() >= 2 {
        // Cluster by reason — pick the most common.
        let mut counts: std::collections::HashMap<&str, usize> = Default::default();
        for f in &last_failures {
            if let Some(reason) = f.failure_reason.as_deref() {
                *counts.entry(reason).or_default() += 1;
            }
        }
        if let Some((reason, count)) = counts.iter().max_by_key(|(_, c)| **c) {
            if *count >= 2 {
                suggested.push(SuggestedAction::InspectRecentFailures {
                    failure_reason: (*reason).to_string(),
                    count: *count,
                });
            }
        }
    }

    if let TriggerKind::Event(e) = &schedule.trigger.kind {
        if !observed_event_paths.contains("event") {
            suggested.push(SuggestedAction::CheckUpstreamEventEmission {
                target_rid: e.target_rid.clone(),
            });
        }
    }

    if let TriggerKind::Time(t) = &schedule.trigger.kind {
        if t.cron.starts_with("*/1 ") || t.cron.starts_with("*/2 ") || t.cron.starts_with("*/3 ") {
            suggested.push(SuggestedAction::ConsiderLowerFrequency {
                cron: t.cron.clone(),
            });
        }
    }

    TroubleshootReport {
        schedule_rid: schedule.rid.clone(),
        trigger_state,
        last_failures,
        suggested_actions: suggested,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::trigger::{
        CompoundOp, CompoundTrigger, CronFlavor, EventTrigger, EventType, ScheduleScopeKind,
        ScheduleTarget, ScheduleTargetKind, SyncRunTarget, TimeTrigger,
    };
    use std::collections::{BTreeMap, HashSet};
    use uuid::Uuid;

    fn schedule_with(trigger: Trigger) -> Schedule {
        Schedule {
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
            created_at: Utc::now(),
            updated_at: Utc::now(),
            last_run_at: None,
            paused_reason: None,
            paused_at: None,
            auto_pause_exempt: false,
            pending_re_run: false,
            active_run_id: None,
            scope_kind: ScheduleScopeKind::User,
            project_scope_rids: vec![],
            run_as_user_id: None,
            service_principal_id: None,
        }
    }

    fn time_trigger() -> Trigger {
        Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: "0 9 * * *".into(),
                time_zone: "UTC".into(),
                flavor: CronFlavor::Unix5,
            }),
        }
    }

    fn event_trigger(rid: &str) -> Trigger {
        Trigger {
            kind: TriggerKind::Event(EventTrigger {
                event_type: EventType::DataUpdated,
                target_rid: rid.into(),
                branch_filter: vec![],
            }),
        }
    }

    fn run(outcome: RunOutcome, reason: Option<&str>) -> ScheduleRun {
        ScheduleRun {
            id: Uuid::now_v7(),
            rid: format!("ri.foundry.main.schedule_run.{}", Uuid::now_v7()),
            schedule_id: Uuid::nil(),
            outcome,
            build_rid: None,
            failure_reason: reason.map(String::from),
            triggered_at: Utc::now(),
            finished_at: Some(Utc::now()),
            trigger_snapshot: BTreeMap::new(),
            schedule_version: 1,
        }
    }

    #[test]
    fn time_trigger_is_always_satisfied() {
        let s = schedule_with(time_trigger());
        let view = compute_trigger_state(&s, &HashSet::new());
        assert!(view.satisfied);
        assert!(view.blocking_components.is_empty());
    }

    #[test]
    fn event_trigger_blocks_when_no_observation() {
        let s = schedule_with(event_trigger("ri.foundry.main.dataset.x"));
        let view = compute_trigger_state(&s, &HashSet::new());
        assert!(!view.satisfied);
        assert_eq!(view.blocking_components.len(), 1);
        assert!(view.blocking_components[0].contains("DataUpdated"));
    }

    #[test]
    fn and_compound_blocks_until_all_observed() {
        let trig = Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::And,
                components: vec![
                    event_trigger("ri.x"),
                    event_trigger("ri.y"),
                ],
            }),
        };
        let s = schedule_with(trig);
        let mut paths = HashSet::new();
        paths.insert("compound[0].event".to_string());
        let view = compute_trigger_state(&s, &paths);
        assert!(!view.satisfied);
        assert_eq!(view.blocking_components.len(), 1);
    }

    #[test]
    fn auto_paused_schedule_emits_resume_suggestion() {
        let mut s = schedule_with(time_trigger());
        s.paused = true;
        s.paused_reason = Some(AUTO_PAUSED_REASON.into());
        let report = troubleshoot_schedule(&s, &HashSet::new(), &[]);
        assert!(report
            .suggested_actions
            .contains(&SuggestedAction::ResumeAfterAutoPause));
    }

    #[test]
    fn manual_paused_schedule_emits_confirm_resume() {
        let mut s = schedule_with(time_trigger());
        s.paused = true;
        s.paused_reason = Some("MANUAL".into());
        let report = troubleshoot_schedule(&s, &HashSet::new(), &[]);
        assert!(report
            .suggested_actions
            .contains(&SuggestedAction::ConfirmManualResume));
    }

    #[test]
    fn repeated_failure_reason_emits_inspect_action() {
        let s = schedule_with(time_trigger());
        let runs = vec![
            run(RunOutcome::Failed, Some("build-service 503: boom")),
            run(RunOutcome::Failed, Some("build-service 503: boom")),
            run(RunOutcome::Succeeded, None),
        ];
        let report = troubleshoot_schedule(&s, &HashSet::new(), &runs);
        let inspect = report
            .suggested_actions
            .iter()
            .find(|a| matches!(a, SuggestedAction::InspectRecentFailures { .. }));
        assert!(inspect.is_some());
    }

    #[test]
    fn pure_event_trigger_emits_upstream_emission_check() {
        let s = schedule_with(event_trigger("ri.foundry.main.dataset.x"));
        let report = troubleshoot_schedule(&s, &HashSet::new(), &[]);
        assert!(report
            .suggested_actions
            .iter()
            .any(|a| matches!(a, SuggestedAction::CheckUpstreamEventEmission { .. })));
    }

    #[test]
    fn high_frequency_cron_emits_lower_frequency_suggestion() {
        let trig = Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: "*/2 * * * *".into(),
                time_zone: "UTC".into(),
                flavor: CronFlavor::Unix5,
            }),
        };
        let s = schedule_with(trig);
        let report = troubleshoot_schedule(&s, &HashSet::new(), &[]);
        assert!(report
            .suggested_actions
            .iter()
            .any(|a| matches!(a, SuggestedAction::ConsiderLowerFrequency { .. })));
    }
}
