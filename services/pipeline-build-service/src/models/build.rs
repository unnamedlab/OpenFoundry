//! Build and BuildState models. Mirrors `proto/pipeline/builds.proto`
//! field-for-field; the variants of [`BuildState`] are the canonical
//! string vocabulary persisted in `builds.state` and re-used by
//! the legacy `pipeline_runs.status` column (see migration
//! `20260504000050_builds_init.sql`).

use std::fmt;
use std::str::FromStr;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

/// Per-build lifecycle. The serde representation matches the proto
/// enum names verbatim so the wire format and the SQL CHECK constraint
/// stay in lockstep.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Hash)]
pub enum BuildState {
    #[serde(rename = "BUILD_RESOLUTION")]
    Resolution,
    #[serde(rename = "BUILD_QUEUED")]
    Queued,
    #[serde(rename = "BUILD_RUNNING")]
    Running,
    #[serde(rename = "BUILD_ABORTING")]
    Aborting,
    #[serde(rename = "BUILD_FAILED")]
    Failed,
    #[serde(rename = "BUILD_ABORTED")]
    Aborted,
    #[serde(rename = "BUILD_COMPLETED")]
    Completed,
}

impl BuildState {
    pub const ALL: &'static [BuildState] = &[
        BuildState::Resolution,
        BuildState::Queued,
        BuildState::Running,
        BuildState::Aborting,
        BuildState::Failed,
        BuildState::Aborted,
        BuildState::Completed,
    ];

    pub fn as_str(&self) -> &'static str {
        match self {
            BuildState::Resolution => "BUILD_RESOLUTION",
            BuildState::Queued => "BUILD_QUEUED",
            BuildState::Running => "BUILD_RUNNING",
            BuildState::Aborting => "BUILD_ABORTING",
            BuildState::Failed => "BUILD_FAILED",
            BuildState::Aborted => "BUILD_ABORTED",
            BuildState::Completed => "BUILD_COMPLETED",
        }
    }

    pub fn is_terminal(&self) -> bool {
        matches!(
            self,
            BuildState::Failed | BuildState::Aborted | BuildState::Completed
        )
    }
}

impl fmt::Display for BuildState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

impl FromStr for BuildState {
    type Err = UnknownBuildState;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "BUILD_RESOLUTION" => Ok(BuildState::Resolution),
            "BUILD_QUEUED" => Ok(BuildState::Queued),
            "BUILD_RUNNING" => Ok(BuildState::Running),
            "BUILD_ABORTING" => Ok(BuildState::Aborting),
            "BUILD_FAILED" => Ok(BuildState::Failed),
            "BUILD_ABORTED" => Ok(BuildState::Aborted),
            "BUILD_COMPLETED" => Ok(BuildState::Completed),
            other => Err(UnknownBuildState(other.to_string())),
        }
    }
}

#[derive(Debug, thiserror::Error)]
#[error("unknown build state: {0}")]
pub struct UnknownBuildState(pub String);

/// Per-build cascade scope, see Foundry Builds.md § Job execution.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Hash)]
pub enum AbortPolicy {
    /// Abort only directly-dependent jobs on first failure (default).
    #[serde(rename = "DEPENDENT_ONLY")]
    DependentOnly,
    /// Abort every non-completed job in the build (including those
    /// independent of the failed job).
    #[serde(rename = "ALL_NON_DEPENDENT")]
    AllNonDependent,
}

impl AbortPolicy {
    pub const ALL: &'static [AbortPolicy] = &[
        AbortPolicy::DependentOnly,
        AbortPolicy::AllNonDependent,
    ];

    pub fn as_str(&self) -> &'static str {
        match self {
            AbortPolicy::DependentOnly => "DEPENDENT_ONLY",
            AbortPolicy::AllNonDependent => "ALL_NON_DEPENDENT",
        }
    }
}

impl Default for AbortPolicy {
    fn default() -> Self {
        AbortPolicy::DependentOnly
    }
}

impl FromStr for AbortPolicy {
    type Err = UnknownAbortPolicy;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "DEPENDENT_ONLY" => Ok(AbortPolicy::DependentOnly),
            "ALL_NON_DEPENDENT" => Ok(AbortPolicy::AllNonDependent),
            other => Err(UnknownAbortPolicy(other.to_string())),
        }
    }
}

#[derive(Debug, thiserror::Error)]
#[error("unknown abort policy: {0}")]
pub struct UnknownAbortPolicy(pub String);

/// Concrete `Build` row. Field names match the proto exactly except for
/// the SQL-only `id`/`created_at` housekeeping columns.
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct Build {
    pub id: Uuid,
    pub rid: String,
    pub pipeline_rid: String,
    pub build_branch: String,
    pub job_spec_fallback: Vec<String>,
    pub state: String,
    pub trigger_kind: String,
    pub force_build: bool,
    pub abort_policy: String,
    pub queued_at: Option<DateTime<Utc>>,
    pub started_at: Option<DateTime<Utc>>,
    pub finished_at: Option<DateTime<Utc>>,
    pub error_message: Option<String>,
    pub requested_by: String,
    pub created_at: DateTime<Utc>,
}

impl Build {
    pub fn build_state(&self) -> Result<BuildState, UnknownBuildState> {
        BuildState::from_str(&self.state)
    }

    pub fn parsed_abort_policy(&self) -> AbortPolicy {
        self.abort_policy
            .parse()
            .unwrap_or(AbortPolicy::DependentOnly)
    }
}

// -- HTTP DTOs -------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct CreateBuildRequest {
    pub pipeline_rid: String,
    pub build_branch: String,
    #[serde(default)]
    pub job_spec_fallback: Vec<String>,
    #[serde(default)]
    pub force_build: bool,
    /// Set of output dataset RIDs the build must produce. The server
    /// looks up the JobSpec for each via [`JobSpecRepo`]; missing
    /// specs surface as `BuildResolutionError::MissingJobSpec`.
    #[serde(default)]
    pub output_dataset_rids: Vec<String>,
    /// MANUAL | SCHEDULED | FORCE — defaults to MANUAL.
    #[serde(default)]
    pub trigger_kind: Option<String>,
    /// DEPENDENT_ONLY (default) | ALL_NON_DEPENDENT.
    #[serde(default)]
    pub abort_policy: Option<AbortPolicy>,
}

#[derive(Debug, Deserialize)]
pub struct ListBuildsQuery {
    pub branch: Option<String>,
    pub status: Option<String>,
    pub pipeline_rid: Option<String>,
    /// ISO-8601 timestamp; only builds with `created_at >= since` are
    /// returned.
    pub since: Option<DateTime<Utc>>,
    /// Cursor-based paging: opaque token returned in `next_cursor`.
    pub cursor: Option<String>,
    pub limit: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct BuildEnvelope {
    #[serde(flatten)]
    pub build: Build,
    pub jobs: Vec<super::job::Job>,
}
