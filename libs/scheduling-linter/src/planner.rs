//! Sweep planner: runs every rule, tags findings with their owners,
//! and (for the `:apply` flow) maps a list of finding ids onto the
//! concrete schedule actions the host service has to execute.

use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::model::{Action, Finding, RuleId, SweepInput};
use crate::rules;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SweepReport {
    pub findings: Vec<Finding>,
}

impl SweepReport {
    pub fn group_by_rule(&self) -> std::collections::BTreeMap<&'static str, Vec<&Finding>> {
        let mut out: std::collections::BTreeMap<&'static str, Vec<&Finding>> = Default::default();
        for f in &self.findings {
            out.entry(f.rule_id.code()).or_default().push(f);
        }
        out
    }

    /// Filter findings by rule + finding ids, returning the actions
    /// the apply pass must execute. Each pair carries enough context
    /// for the host service to call its own pause / archive / delete
    /// primitive.
    pub fn plan_apply(&self, rule_ids: &[RuleId], finding_ids: &[Uuid]) -> Vec<AppliedAction> {
        self.findings
            .iter()
            .filter(|f| rule_ids.is_empty() || rule_ids.contains(&f.rule_id))
            .filter(|f| finding_ids.is_empty() || finding_ids.contains(&f.id))
            .map(|f| AppliedAction {
                finding_id: f.id,
                schedule_rid: f.schedule_rid.clone(),
                action: f.recommended_action,
            })
            .collect()
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AppliedAction {
    pub finding_id: Uuid,
    pub schedule_rid: String,
    pub action: Action,
}

/// Run every rule against `input`. The order of findings reflects the
/// rule ordering in the catalogue (SCH-001 through SCH-007) so the UI
/// can render a stable, deterministic table.
pub fn run_sweep(input: &SweepInput) -> SweepReport {
    let mut findings = Vec::new();
    findings.extend(rules::apply_sch001(input));
    findings.extend(rules::apply_sch002(input));
    findings.extend(rules::apply_sch003(input));
    findings.extend(rules::apply_sch004(input));
    findings.extend(rules::apply_sch005(input));
    findings.extend(rules::apply_sch006(input));
    findings.extend(rules::apply_sch007(input));
    SweepReport { findings }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::*;
    use chrono::{Duration, TimeZone, Utc};

    fn input_with(schedule: InventorySchedule) -> SweepInput {
        SweepInput {
            schedules: vec![schedule],
            now: Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap(),
            production: true,
        }
    }

    #[test]
    fn run_sweep_collects_findings_in_rule_order() {
        let now = Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap();
        let s = InventorySchedule {
            id: Uuid::now_v7(),
            rid: "ri.s.1".into(),
            project_rid: "ri.p.1".into(),
            name: "noisy".into(),
            paused: true,
            paused_at: Some(now - Duration::days(45)),
            scope_kind: "USER".into(),
            run_as_user: Some(InventoryUser {
                id: Uuid::now_v7(),
                display_name: "alice".into(),
                active: true,
                last_login_at: Some(now - Duration::days(45)),
            }),
            trigger: InventoryTrigger::Event {
                target_rid: "ri.x".into(),
                branch_filter: vec![],
            },
            recent_runs: vec![],
        };
        let report = run_sweep(&input_with(s));
        // SCH-001 (no recent runs), SCH-002 (paused), SCH-005 (stale owner),
        // SCH-007 (event w/o filter).
        let codes: Vec<&'static str> = report.findings.iter().map(|f| f.rule_id.code()).collect();
        assert!(codes.contains(&"SCH-001"));
        assert!(codes.contains(&"SCH-002"));
        assert!(codes.contains(&"SCH-005"));
        assert!(codes.contains(&"SCH-007"));
    }

    #[test]
    fn plan_apply_filters_by_finding_id() {
        let report = SweepReport {
            findings: vec![
                Finding {
                    id: Uuid::nil(),
                    rule_id: RuleId::Sch001InactiveLastNinety,
                    severity: Severity::Warning,
                    schedule_rid: "ri.s.1".into(),
                    project_rid: "ri.p.1".into(),
                    message: "".into(),
                    recommended_action: Action::Pause,
                },
                Finding {
                    id: Uuid::from_u128(1),
                    rule_id: RuleId::Sch003HighFailureRate,
                    severity: Severity::Error,
                    schedule_rid: "ri.s.2".into(),
                    project_rid: "ri.p.1".into(),
                    message: "".into(),
                    recommended_action: Action::Notify,
                },
            ],
        };
        let plan = report.plan_apply(&[], &[Uuid::nil()]);
        assert_eq!(plan.len(), 1);
        assert_eq!(plan[0].action, Action::Pause);
    }
}
