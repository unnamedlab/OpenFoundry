use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Copy, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum ScheduleTargetKind {
    Pipeline,
    Workflow,
}

#[derive(Debug, Deserialize)]
pub struct ListDueRunsQuery {
    pub kind: Option<ScheduleTargetKind>,
    pub limit: Option<usize>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DueRunRecord {
    pub target_kind: ScheduleTargetKind,
    pub target_id: Uuid,
    pub name: String,
    pub due_at: DateTime<Utc>,
    pub schedule_expression: String,
    pub trigger_type: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScheduleWindow {
    pub scheduled_for: DateTime<Utc>,
    pub window_start: DateTime<Utc>,
    pub window_end: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct PreviewScheduleWindowsRequest {
    pub target_kind: ScheduleTargetKind,
    pub target_id: Uuid,
    pub start_at: DateTime<Utc>,
    pub end_at: DateTime<Utc>,
    pub limit: Option<usize>,
}

fn default_true() -> bool {
    true
}

fn default_skip_unchanged() -> bool {
    true
}

#[derive(Debug, Deserialize)]
pub struct BackfillScheduleRequest {
    pub target_kind: ScheduleTargetKind,
    pub target_id: Uuid,
    pub start_at: DateTime<Utc>,
    pub end_at: DateTime<Utc>,
    pub limit: Option<usize>,
    #[serde(default = "default_true")]
    pub dry_run: bool,
    #[serde(default)]
    pub context: Option<Value>,
    #[serde(default = "default_skip_unchanged")]
    pub skip_unchanged: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct BackfillRunResult {
    pub target_kind: ScheduleTargetKind,
    pub target_id: Uuid,
    pub scheduled_for: DateTime<Utc>,
    pub window_start: DateTime<Utc>,
    pub window_end: DateTime<Utc>,
    pub run_id: Option<Uuid>,
    pub status: String,
}
