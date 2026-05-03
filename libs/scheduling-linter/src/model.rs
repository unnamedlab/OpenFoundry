//! Inventory snapshot fed to the rule engine. The host service
//! materialises one [`SweepInput`] per sweep call by joining
//! `schedules`, `schedule_runs`, and a user-directory query.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum Severity {
    Info,
    Warning,
    Error,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum RuleId {
    Sch001InactiveLastNinety,
    Sch002PausedLongerThanThirty,
    Sch003HighFailureRate,
    Sch004OwnerInactive,
    Sch005UserScopeOwnerStale,
    Sch006HighFrequencyCron,
    Sch007EventTriggerWithoutBranchFilter,
}

impl RuleId {
    pub fn code(&self) -> &'static str {
        match self {
            RuleId::Sch001InactiveLastNinety => "SCH-001",
            RuleId::Sch002PausedLongerThanThirty => "SCH-002",
            RuleId::Sch003HighFailureRate => "SCH-003",
            RuleId::Sch004OwnerInactive => "SCH-004",
            RuleId::Sch005UserScopeOwnerStale => "SCH-005",
            RuleId::Sch006HighFrequencyCron => "SCH-006",
            RuleId::Sch007EventTriggerWithoutBranchFilter => "SCH-007",
        }
    }
}

/// Recommended action attached to every [`Finding`]. The sweep:apply
/// endpoint maps these onto concrete schedule-store calls.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum Action {
    /// No automatic remediation — alert the owner only.
    Notify,
    /// Pause the schedule (preserving the row). Used by SCH-001 / 002.
    Pause,
    /// Hard delete the schedule. Used by SCH-004 (orphaned owner).
    Delete,
    /// Archive the schedule (paused + archived flag). For schedules
    /// the operator wants kept around as audit history.
    Archive,
}

/// A single rule-violation report.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Finding {
    pub id: Uuid,
    pub rule_id: RuleId,
    pub severity: Severity,
    pub schedule_rid: String,
    pub project_rid: String,
    pub message: String,
    pub recommended_action: Action,
}

/// Full inventory the rule engine inspects. Pure-data; no DB / network.
#[derive(Debug, Clone, Default)]
pub struct SweepInput {
    pub schedules: Vec<InventorySchedule>,
    pub now: DateTime<Utc>,
    /// Whether the host is the production environment — gates SCH-006.
    pub production: bool,
}

#[derive(Debug, Clone)]
pub struct InventorySchedule {
    pub id: Uuid,
    pub rid: String,
    pub project_rid: String,
    pub name: String,
    pub paused: bool,
    pub paused_at: Option<DateTime<Utc>>,
    pub scope_kind: String,
    pub run_as_user: Option<InventoryUser>,
    pub trigger: InventoryTrigger,
    pub recent_runs: Vec<InventoryRun>,
}

/// Materialised view of the trigger surface — only the bits the
/// linter inspects. Keeps the linter independent of the canonical
/// `pipeline_schedule_service::domain::trigger` shape so the same
/// rule library can be reused by the pipeline-build-service or the
/// ops console without dragging in the whole schedule service.
#[derive(Debug, Clone)]
pub enum InventoryTrigger {
    /// `time_zone` is parsed by the SCH-006 rule when checking
    /// frequency.
    Time {
        cron: String,
        time_zone: String,
        flavor: TriggerCronFlavor,
    },
    Event {
        target_rid: String,
        branch_filter: Vec<String>,
    },
    /// Compound triggers fan out into individual leaves. The linter
    /// recurses into them via the [`InventoryTrigger::leaves`] helper.
    Compound {
        children: Vec<InventoryTrigger>,
    },
}

#[derive(Debug, Clone, Copy)]
pub enum TriggerCronFlavor {
    Unix5,
    Quartz6,
}

impl InventoryTrigger {
    /// Yield every leaf trigger under this node, depth-first.
    pub fn leaves(&self) -> Vec<&InventoryTrigger> {
        let mut out = Vec::new();
        fn recurse<'a>(t: &'a InventoryTrigger, out: &mut Vec<&'a InventoryTrigger>) {
            match t {
                InventoryTrigger::Compound { children } => {
                    for c in children {
                        recurse(c, out);
                    }
                }
                _ => out.push(t),
            }
        }
        recurse(self, &mut out);
        out
    }
}

#[derive(Debug, Clone)]
pub struct InventoryRun {
    pub triggered_at: DateTime<Utc>,
    /// `"SUCCEEDED" | "IGNORED" | "FAILED"` — kept as &'static for
    /// callers using the `pipeline_schedule_service` shape, but stored
    /// as `String` so the linter's wire model can stay independent.
    pub outcome: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InventoryUser {
    pub id: Uuid,
    pub display_name: String,
    pub active: bool,
    /// `None` when the user has never logged in (e.g. service identity).
    pub last_login_at: Option<DateTime<Utc>>,
}
