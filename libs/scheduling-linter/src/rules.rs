//! Rule implementations. Each `apply_*` walks the inventory and
//! returns the [`Finding`]s its rule produced; the planner stitches
//! them together. Splitting one rule per function keeps the
//! per-rule unit tests narrow.

use std::str::FromStr;

use chrono::Duration;
use chrono_tz::Tz;
use scheduling_cron::{CronFlavor, next_fire_after, parse_cron};
use uuid::Uuid;

use crate::model::{
    Action, Finding, InventorySchedule, InventoryTrigger, RuleId, Severity, SweepInput,
    TriggerCronFlavor,
};

/// SCH-001 — schedule with no runs in the last 90 days.
pub fn apply_sch001(input: &SweepInput) -> Vec<Finding> {
    let cutoff = input.now - Duration::days(90);
    input
        .schedules
        .iter()
        .filter(|s| {
            s.recent_runs
                .iter()
                .all(|r| r.triggered_at < cutoff)
        })
        .map(|s| Finding {
            id: Uuid::now_v7(),
            rule_id: RuleId::Sch001InactiveLastNinety,
            severity: Severity::Warning,
            schedule_rid: s.rid.clone(),
            project_rid: s.project_rid.clone(),
            message: "Schedule has not run in the last 90 days. Consider pausing or archiving."
                .into(),
            recommended_action: Action::Pause,
        })
        .collect()
}

/// SCH-002 — schedule paused longer than 30 days.
pub fn apply_sch002(input: &SweepInput) -> Vec<Finding> {
    let cutoff = input.now - Duration::days(30);
    input
        .schedules
        .iter()
        .filter(|s| s.paused && s.paused_at.map(|t| t < cutoff).unwrap_or(false))
        .map(|s| Finding {
            id: Uuid::now_v7(),
            rule_id: RuleId::Sch002PausedLongerThanThirty,
            severity: Severity::Warning,
            schedule_rid: s.rid.clone(),
            project_rid: s.project_rid.clone(),
            message: "Schedule has been paused for more than 30 days.".into(),
            recommended_action: Action::Archive,
        })
        .collect()
}

/// SCH-003 — failure rate > 50% over the last 30 days.
pub fn apply_sch003(input: &SweepInput) -> Vec<Finding> {
    let window_start = input.now - Duration::days(30);
    input
        .schedules
        .iter()
        .filter_map(|s| {
            let runs_in_window: Vec<_> = s
                .recent_runs
                .iter()
                .filter(|r| r.triggered_at >= window_start)
                .collect();
            if runs_in_window.is_empty() {
                return None;
            }
            let failures = runs_in_window
                .iter()
                .filter(|r| r.outcome == "FAILED")
                .count();
            // Strict majority — `> 50 %`.
            if failures * 2 > runs_in_window.len() {
                Some(Finding {
                    id: Uuid::now_v7(),
                    rule_id: RuleId::Sch003HighFailureRate,
                    severity: Severity::Error,
                    schedule_rid: s.rid.clone(),
                    project_rid: s.project_rid.clone(),
                    message: format!(
                        "{} of {} runs in the last 30 days failed.",
                        failures,
                        runs_in_window.len()
                    ),
                    recommended_action: Action::Notify,
                })
            } else {
                None
            }
        })
        .collect()
}

/// SCH-004 — owner deactivated.
pub fn apply_sch004(input: &SweepInput) -> Vec<Finding> {
    input
        .schedules
        .iter()
        .filter_map(|s| {
            let owner = s.run_as_user.as_ref()?;
            (!owner.active).then(|| Finding {
                id: Uuid::now_v7(),
                rule_id: RuleId::Sch004OwnerInactive,
                severity: Severity::Error,
                schedule_rid: s.rid.clone(),
                project_rid: s.project_rid.clone(),
                message: format!(
                    "Owner '{}' is deactivated; schedule cannot run as that user.",
                    owner.display_name
                ),
                recommended_action: Action::Delete,
            })
        })
        .collect()
}

/// SCH-005 — USER-mode schedule whose owner has not logged in for > 30 days.
pub fn apply_sch005(input: &SweepInput) -> Vec<Finding> {
    let cutoff = input.now - Duration::days(30);
    input
        .schedules
        .iter()
        .filter_map(|s| {
            if s.scope_kind != "USER" {
                return None;
            }
            let owner = s.run_as_user.as_ref()?;
            let last = owner.last_login_at?;
            (last < cutoff).then(|| Finding {
                id: Uuid::now_v7(),
                rule_id: RuleId::Sch005UserScopeOwnerStale,
                severity: Severity::Warning,
                schedule_rid: s.rid.clone(),
                project_rid: s.project_rid.clone(),
                message: format!(
                    "Owner '{}' has not signed in for over 30 days. Consider converting the schedule to PROJECT_SCOPED.",
                    owner.display_name
                ),
                recommended_action: Action::Notify,
            })
        })
        .collect()
}

/// SCH-006 — production schedule firing more often than every 5 minutes.
pub fn apply_sch006(input: &SweepInput) -> Vec<Finding> {
    if !input.production {
        return Vec::new();
    }
    let mut findings = Vec::new();
    for s in &input.schedules {
        for leaf in s.trigger.leaves() {
            if let InventoryTrigger::Time {
                cron,
                time_zone,
                flavor,
            } = leaf
            {
                if interval_under_5_minutes(cron, time_zone, *flavor) {
                    findings.push(Finding {
                        id: Uuid::now_v7(),
                        rule_id: RuleId::Sch006HighFrequencyCron,
                        severity: Severity::Warning,
                        schedule_rid: s.rid.clone(),
                        project_rid: s.project_rid.clone(),
                        message: format!(
                            "Production schedule fires more often than every 5 minutes (cron `{cron}`)."
                        ),
                        recommended_action: Action::Notify,
                    });
                }
            }
        }
    }
    findings
}

/// SCH-007 — Event-trigger leaf with no `branch_filter`.
pub fn apply_sch007(input: &SweepInput) -> Vec<Finding> {
    let mut findings = Vec::new();
    for s in &input.schedules {
        for leaf in s.trigger.leaves() {
            if let InventoryTrigger::Event { branch_filter, target_rid } = leaf {
                if branch_filter.is_empty() {
                    findings.push(Finding {
                        id: Uuid::now_v7(),
                        rule_id: RuleId::Sch007EventTriggerWithoutBranchFilter,
                        severity: Severity::Info,
                        schedule_rid: s.rid.clone(),
                        project_rid: s.project_rid.clone(),
                        message: format!(
                            "Event trigger on `{target_rid}` has no branch_filter; will fire on every branch."
                        ),
                        recommended_action: Action::Notify,
                    });
                }
            }
        }
    }
    findings
}

/// Compute whether `cron` fires more than once per 5-minute window
/// somewhere in the next hour. Two consecutive fires < 5 minutes apart
/// is sufficient evidence; we don't need to enumerate the full schedule.
fn interval_under_5_minutes(cron: &str, time_zone: &str, flavor: TriggerCronFlavor) -> bool {
    let tz = match Tz::from_str(time_zone) {
        Ok(tz) => tz,
        Err(_) => return false,
    };
    let parser_flavor = match flavor {
        TriggerCronFlavor::Unix5 => CronFlavor::Unix5,
        TriggerCronFlavor::Quartz6 => CronFlavor::Quartz6,
    };
    let parsed = match parse_cron(cron, parser_flavor, tz) {
        Ok(p) => p,
        Err(_) => return false,
    };
    let now = chrono::Utc::now();
    let first = match next_fire_after(&parsed, now) {
        Some(t) => t,
        None => return false,
    };
    let second = match next_fire_after(&parsed, first) {
        Some(t) => t,
        None => return false,
    };
    (second - first) < Duration::minutes(5)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::*;
    use chrono::TimeZone;

    fn schedule_fixture(name: &str) -> InventorySchedule {
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
                last_login_at: None,
            }),
            trigger: InventoryTrigger::Time {
                cron: "0 9 * * *".into(),
                time_zone: "UTC".into(),
                flavor: TriggerCronFlavor::Unix5,
            },
            recent_runs: vec![],
        }
    }

    fn input_at(now: chrono::DateTime<chrono::Utc>, schedules: Vec<InventorySchedule>) -> SweepInput {
        SweepInput {
            schedules,
            now,
            production: true,
        }
    }

    #[test]
    fn sch001_flags_schedule_with_no_recent_runs() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let s = schedule_fixture("idle");
        let findings = apply_sch001(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
        assert_eq!(findings[0].rule_id, RuleId::Sch001InactiveLastNinety);
        assert_eq!(findings[0].recommended_action, Action::Pause);
    }

    #[test]
    fn sch001_skips_schedule_with_recent_run() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("active");
        s.recent_runs.push(InventoryRun {
            triggered_at: now - Duration::days(1),
            outcome: "SUCCEEDED".into(),
        });
        let findings = apply_sch001(&input_at(now, vec![s]));
        assert!(findings.is_empty());
    }

    #[test]
    fn sch002_flags_schedule_paused_more_than_30_days() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("p");
        s.paused = true;
        s.paused_at = Some(now - Duration::days(45));
        let findings = apply_sch002(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
        assert_eq!(findings[0].recommended_action, Action::Archive);
    }

    #[test]
    fn sch003_flags_high_failure_rate() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("flaky");
        s.recent_runs = vec![
            InventoryRun {
                triggered_at: now - Duration::days(2),
                outcome: "FAILED".into(),
            },
            InventoryRun {
                triggered_at: now - Duration::days(3),
                outcome: "FAILED".into(),
            },
            InventoryRun {
                triggered_at: now - Duration::days(4),
                outcome: "SUCCEEDED".into(),
            },
        ];
        let findings = apply_sch003(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
        assert_eq!(findings[0].severity, Severity::Error);
    }

    #[test]
    fn sch003_does_not_flag_schedule_under_threshold() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("ok");
        s.recent_runs = vec![
            InventoryRun {
                triggered_at: now - Duration::days(2),
                outcome: "FAILED".into(),
            },
            InventoryRun {
                triggered_at: now - Duration::days(3),
                outcome: "SUCCEEDED".into(),
            },
        ];
        let findings = apply_sch003(&input_at(now, vec![s]));
        assert!(findings.is_empty());
    }

    #[test]
    fn sch004_flags_orphaned_owner() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("orphan");
        s.run_as_user.as_mut().unwrap().active = false;
        let findings = apply_sch004(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
        assert_eq!(findings[0].recommended_action, Action::Delete);
    }

    #[test]
    fn sch005_flags_user_scope_with_stale_owner() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("stale");
        s.run_as_user.as_mut().unwrap().last_login_at = Some(now - Duration::days(60));
        let findings = apply_sch005(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
    }

    #[test]
    fn sch005_skips_project_scoped_schedules() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("ps");
        s.scope_kind = "PROJECT_SCOPED".into();
        s.run_as_user.as_mut().unwrap().last_login_at = Some(now - Duration::days(60));
        let findings = apply_sch005(&input_at(now, vec![s]));
        assert!(findings.is_empty());
    }

    #[test]
    fn sch006_flags_high_frequency_cron_in_production() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("freq");
        s.trigger = InventoryTrigger::Time {
            cron: "*/2 * * * *".into(),
            time_zone: "UTC".into(),
            flavor: TriggerCronFlavor::Unix5,
        };
        let findings = apply_sch006(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
    }

    #[test]
    fn sch006_skips_when_not_production() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("dev");
        s.trigger = InventoryTrigger::Time {
            cron: "*/2 * * * *".into(),
            time_zone: "UTC".into(),
            flavor: TriggerCronFlavor::Unix5,
        };
        let mut input = input_at(now, vec![s]);
        input.production = false;
        let findings = apply_sch006(&input);
        assert!(findings.is_empty());
    }

    #[test]
    fn sch007_flags_event_trigger_without_branch_filter() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("ev");
        s.trigger = InventoryTrigger::Event {
            target_rid: "ri.foundry.main.dataset.x".into(),
            branch_filter: vec![],
        };
        let findings = apply_sch007(&input_at(now, vec![s]));
        assert_eq!(findings.len(), 1);
    }

    #[test]
    fn sch007_skips_event_trigger_with_branch_filter() {
        let now = chrono::Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let mut s = schedule_fixture("ev-filtered");
        s.trigger = InventoryTrigger::Event {
            target_rid: "ri.foundry.main.dataset.x".into(),
            branch_filter: vec!["master".into()],
        };
        let findings = apply_sch007(&input_at(now, vec![s]));
        assert!(findings.is_empty());
    }
}
