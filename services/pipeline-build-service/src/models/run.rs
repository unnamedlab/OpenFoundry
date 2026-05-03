use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

fn default_skip_unchanged() -> bool {
    true
}

/// Legacy per-pipeline run row.
///
/// `status` was originally a free-form string (`pending`, `running`,
/// `completed`, `failed`, `aborted`); the `20260504000050_builds_init`
/// migration normalises existing rows to the new BuildState vocabulary
/// and the serde aliases below let in-flight readers handle either
/// representation. New writers always use the canonical
/// `BuildState::as_str()` value.
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct PipelineRun {
    pub id: Uuid,
    pub pipeline_id: Uuid,
    /// Stored as TEXT; canonical form is the proto `BuildState` enum
    /// name (`BUILD_RUNNING`, ...). Use [`PipelineRun::build_state`] to
    /// project to a typed value.
    pub status: String,
    pub trigger_type: String,
    pub started_by: Option<Uuid>,
    pub attempt_number: i32,
    pub started_from_node_id: Option<String>,
    pub retry_of_run_id: Option<Uuid>,
    pub execution_context: Value,
    pub node_results: Option<serde_json::Value>,
    pub error_message: Option<String>,
    pub started_at: DateTime<Utc>,
    pub finished_at: Option<DateTime<Utc>>,
}

impl PipelineRun {
    /// Best-effort projection of the legacy `status` column to the new
    /// `BuildState` vocabulary. Falls back to `BuildState::Running` for
    /// values that do not map cleanly so the queue UI does not blow up
    /// on stale rows mid-deploy.
    pub fn build_state(&self) -> crate::models::build::BuildState {
        use crate::models::build::BuildState;
        match self.status.as_str() {
            // Canonical values (post-migration).
            "BUILD_RESOLUTION" => BuildState::Resolution,
            "BUILD_QUEUED" => BuildState::Queued,
            "BUILD_RUNNING" => BuildState::Running,
            "BUILD_ABORTING" => BuildState::Aborting,
            "BUILD_FAILED" => BuildState::Failed,
            "BUILD_ABORTED" => BuildState::Aborted,
            "BUILD_COMPLETED" => BuildState::Completed,
            // Legacy values (pre-migration safety net).
            "pending" => BuildState::Queued,
            "running" => BuildState::Running,
            "completed" => BuildState::Completed,
            "failed" => BuildState::Failed,
            "aborted" => BuildState::Aborted,
            _ => BuildState::Running,
        }
    }
}

#[derive(Debug, Deserialize)]
pub struct ListRunsQuery {
    pub page: Option<i64>,
    pub per_page: Option<i64>,
}

#[derive(Debug, Deserialize)]
pub struct TriggerPipelineRequest {
    pub from_node_id: Option<String>,
    pub context: Option<Value>,
    #[serde(default = "default_skip_unchanged")]
    pub skip_unchanged: bool,
}

#[derive(Debug, Deserialize)]
pub struct RetryPipelineRunRequest {
    pub from_node_id: Option<String>,
    #[serde(default = "default_skip_unchanged")]
    pub skip_unchanged: bool,
}
