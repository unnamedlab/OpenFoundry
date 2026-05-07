//! Schedule trigger model — Rust-side mirror of the proto messages
//! defined in `proto/pipeline/schedules.proto`. Persisted as JSONB
//! in `schedules.trigger_json` and `schedules.target_json`.

use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// Top-level trigger envelope. The `kind` discriminator matches the
/// proto `oneof Trigger.kind` exactly.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "snake_case")]
pub struct Trigger {
    pub kind: TriggerKind,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum TriggerKind {
    Time(TimeTrigger),
    Event(EventTrigger),
    Compound(CompoundTrigger),
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct TimeTrigger {
    pub cron: String,
    #[serde(default = "default_utc")]
    pub time_zone: String,
    pub flavor: CronFlavor,
}

fn default_utc() -> String {
    "UTC".to_string()
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum CronFlavor {
    Unix5,
    Quartz6,
}

impl From<CronFlavor> for scheduling_cron::CronFlavor {
    fn from(f: CronFlavor) -> Self {
        match f {
            CronFlavor::Unix5 => scheduling_cron::CronFlavor::Unix5,
            CronFlavor::Quartz6 => scheduling_cron::CronFlavor::Quartz6,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct EventTrigger {
    #[serde(rename = "type")]
    pub event_type: EventType,
    pub target_rid: String,
    #[serde(default)]
    pub branch_filter: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum EventType {
    NewLogic,
    DataUpdated,
    JobSucceeded,
    ScheduleRanSuccessfully,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct CompoundTrigger {
    pub op: CompoundOp,
    pub components: Vec<Trigger>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum CompoundOp {
    And,
    Or,
}

// ---- Schedule target -------------------------------------------------------

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct ScheduleTarget {
    pub kind: ScheduleTargetKind,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ScheduleTargetKind {
    PipelineBuild(PipelineBuildTarget),
    DatasetBuild(DatasetBuildTarget),
    SyncRun(SyncRunTarget),
    HealthCheck(HealthCheckTarget),
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct PipelineBuildTarget {
    pub pipeline_rid: String,
    pub build_branch: String,
    #[serde(default)]
    pub job_spec_fallback: Vec<String>,
    #[serde(default)]
    pub force_build: bool,
    #[serde(default)]
    pub abort_policy: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct DatasetBuildTarget {
    pub dataset_rid: String,
    pub build_branch: String,
    #[serde(default)]
    pub force_build: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct SyncRunTarget {
    pub sync_rid: String,
    pub source_rid: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct HealthCheckTarget {
    pub check_rid: String,
}

// ---- Schedule row in memory ------------------------------------------------

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct Schedule {
    pub id: Uuid,
    pub rid: String,
    pub project_rid: String,
    pub name: String,
    pub description: String,
    pub trigger: Trigger,
    pub target: ScheduleTarget,
    pub paused: bool,
    pub version: i32,
    pub created_by: String,
    pub created_at: chrono::DateTime<chrono::Utc>,
    pub updated_at: chrono::DateTime<chrono::Utc>,
    pub last_run_at: Option<chrono::DateTime<chrono::Utc>>,
    /// Reason the schedule was paused. `Some("AUTO_PAUSED_AFTER_FAILURES")`
    /// flags a row paused by the auto-pause supervisor; `Some("MANUAL")`
    /// flags an operator-initiated pause; `None` only when `paused == false`.
    #[serde(default)]
    pub paused_reason: Option<String>,
    #[serde(default)]
    pub paused_at: Option<chrono::DateTime<chrono::Utc>>,
    /// When `true`, the auto-pause supervisor never paused this schedule
    /// (manual pause still works). Set via the
    /// `:exempt-from-auto-pause` endpoint by users with the manage role.
    #[serde(default)]
    pub auto_pause_exempt: bool,
    /// Coalesce flag. Per the doc: "If a schedule is triggered while
    /// the previous run is still in action, it will remain triggered
    /// and run only after the previous schedule is finished."
    #[serde(default)]
    pub pending_re_run: bool,
    /// `Some(run_id)` while a run is in flight; used by the dispatcher
    /// to coalesce concurrent triggers into the post-run re-run flag.
    #[serde(default)]
    pub active_run_id: Option<Uuid>,
    /// P3 — project-scope discriminator. Defaults to `User` for every
    /// row migrated in from P2.
    #[serde(default = "default_user_scope")]
    pub scope_kind: ScheduleScopeKind,
    /// Set of Project RIDs whose union of permissions the schedule
    /// runs against. Always empty under `ScheduleScopeKind::User`.
    #[serde(default)]
    pub project_scope_rids: Vec<String>,
    /// User identity the schedule runs as in `User` mode. `None` in
    /// `ProjectScoped` mode.
    #[serde(default)]
    pub run_as_user_id: Option<Uuid>,
    /// Service-principal identity the schedule runs as in
    /// `ProjectScoped` mode. `None` in `User` mode.
    #[serde(default)]
    pub service_principal_id: Option<Uuid>,
}

fn default_user_scope() -> ScheduleScopeKind {
    ScheduleScopeKind::User
}

/// Auto-pause supervisor reason marker — kept as a constant so callers
/// match exactly on the same value the migration's CHECK / the
/// notification template reads back.
pub const AUTO_PAUSED_REASON: &str = "AUTO_PAUSED_AFTER_FAILURES";
pub const MANUAL_PAUSED_REASON: &str = "MANUAL";

/// Project-scope discriminator.
///
/// Per the Foundry doc § "Project scope":
///   * `User` — the schedule runs as a specific user; AuthZ uses that
///     user's clearance / markings, and a permissions change can break
///     the run.
///   * `ProjectScoped` — the schedule runs against a service principal
///     bound to a set of Projects; AuthZ uses the union of those
///     Projects' clearances and is therefore stable across user
///     permission changes.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ScheduleScopeKind {
    User,
    ProjectScoped,
}

impl ScheduleScopeKind {
    pub fn as_str(&self) -> &'static str {
        match self {
            ScheduleScopeKind::User => "USER",
            ScheduleScopeKind::ProjectScoped => "PROJECT_SCOPED",
        }
    }

    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "USER" => Some(ScheduleScopeKind::User),
            "PROJECT_SCOPED" => Some(ScheduleScopeKind::ProjectScoped),
            _ => None,
        }
    }
}

impl Trigger {
    /// Walk every leaf of the trigger tree, yielding `(path, leaf)` pairs.
    /// `path` is the dotted/bracketed selector recorded in
    /// `schedule_event_observations.trigger_path`, e.g. `"event"` or
    /// `"compound[0].compound[1].event"`. Useful for the trigger
    /// engine to address individual event leaves.
    pub fn walk_leaves<'a>(&'a self) -> Vec<(String, &'a TriggerKind)> {
        fn recur<'a>(t: &'a Trigger, prefix: &str, out: &mut Vec<(String, &'a TriggerKind)>) {
            match &t.kind {
                TriggerKind::Time(_) => {
                    out.push((leaf_path(prefix, "time"), &t.kind));
                }
                TriggerKind::Event(_) => {
                    out.push((leaf_path(prefix, "event"), &t.kind));
                }
                TriggerKind::Compound(c) => {
                    for (idx, child) in c.components.iter().enumerate() {
                        let segment = format!("compound[{idx}]");
                        let next_prefix = if prefix.is_empty() {
                            segment
                        } else {
                            format!("{prefix}.{segment}")
                        };
                        recur(child, &next_prefix, out);
                    }
                }
            }
        }
        let mut out = Vec::new();
        recur(self, "", &mut out);
        out
    }

    pub fn is_pure_time(&self) -> bool {
        matches!(self.kind, TriggerKind::Time(_))
    }
}

fn leaf_path(prefix: &str, leaf: &str) -> String {
    if prefix.is_empty() {
        leaf.to_string()
    } else {
        format!("{prefix}.{leaf}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn time_trigger_round_trips_through_json() {
        let t = Trigger {
            kind: TriggerKind::Time(TimeTrigger {
                cron: "0 9 * * 1".into(),
                time_zone: "America/New_York".into(),
                flavor: CronFlavor::Unix5,
            }),
        };
        let json = serde_json::to_value(&t).unwrap();
        let back: Trigger = serde_json::from_value(json).unwrap();
        assert_eq!(t, back);
    }

    #[test]
    fn compound_trigger_walks_leaves_with_paths() {
        let t = Trigger {
            kind: TriggerKind::Compound(CompoundTrigger {
                op: CompoundOp::And,
                components: vec![
                    Trigger {
                        kind: TriggerKind::Time(TimeTrigger {
                            cron: "0 0 * * *".into(),
                            time_zone: "UTC".into(),
                            flavor: CronFlavor::Unix5,
                        }),
                    },
                    Trigger {
                        kind: TriggerKind::Event(EventTrigger {
                            event_type: EventType::DataUpdated,
                            target_rid: "ri.foundry.main.dataset.x".into(),
                            branch_filter: vec![],
                        }),
                    },
                ],
            }),
        };
        let leaves: Vec<_> = t.walk_leaves().into_iter().map(|(p, _)| p).collect();
        assert_eq!(leaves, vec!["compound[0].time", "compound[1].event"]);
    }
}
