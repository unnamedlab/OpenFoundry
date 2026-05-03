//! Job and JobState models. Mirrors `proto/pipeline/builds.proto`.
//!
//! `JobState` is the Foundry "Builds.md § Job states" vocabulary
//! literally — `WAITING`, `RUN_PENDING`, `RUNNING`, `ABORT_PENDING`,
//! `ABORTED`, `FAILED`, `COMPLETED`. The serde rename keeps the JSON
//! and the SQL CHECK constraint aligned with the proto enum names.

use std::fmt;
use std::str::FromStr;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum JobState {
    #[serde(rename = "WAITING")]
    Waiting,
    #[serde(rename = "RUN_PENDING")]
    RunPending,
    #[serde(rename = "RUNNING")]
    Running,
    #[serde(rename = "ABORT_PENDING")]
    AbortPending,
    #[serde(rename = "ABORTED")]
    Aborted,
    #[serde(rename = "FAILED")]
    Failed,
    #[serde(rename = "COMPLETED")]
    Completed,
}

impl JobState {
    pub const ALL: &'static [JobState] = &[
        JobState::Waiting,
        JobState::RunPending,
        JobState::Running,
        JobState::AbortPending,
        JobState::Aborted,
        JobState::Failed,
        JobState::Completed,
    ];

    pub fn as_str(&self) -> &'static str {
        match self {
            JobState::Waiting => "WAITING",
            JobState::RunPending => "RUN_PENDING",
            JobState::Running => "RUNNING",
            JobState::AbortPending => "ABORT_PENDING",
            JobState::Aborted => "ABORTED",
            JobState::Failed => "FAILED",
            JobState::Completed => "COMPLETED",
        }
    }

    pub fn is_terminal(&self) -> bool {
        matches!(
            self,
            JobState::Aborted | JobState::Failed | JobState::Completed
        )
    }
}

impl fmt::Display for JobState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

impl FromStr for JobState {
    type Err = UnknownJobState;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "WAITING" => Ok(JobState::Waiting),
            "RUN_PENDING" => Ok(JobState::RunPending),
            "RUNNING" => Ok(JobState::Running),
            "ABORT_PENDING" => Ok(JobState::AbortPending),
            "ABORTED" => Ok(JobState::Aborted),
            "FAILED" => Ok(JobState::Failed),
            "COMPLETED" => Ok(JobState::Completed),
            other => Err(UnknownJobState(other.to_string())),
        }
    }
}

#[derive(Debug, thiserror::Error)]
#[error("unknown job state: {0}")]
pub struct UnknownJobState(pub String);

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct Job {
    pub id: Uuid,
    pub rid: String,
    pub build_id: Uuid,
    pub job_spec_rid: String,
    pub state: String,
    pub output_transaction_rids: Vec<String>,
    pub state_changed_at: DateTime<Utc>,
    pub attempt: i32,
    pub stale_skipped: bool,
    pub failure_reason: Option<String>,
    pub output_content_hash: Option<String>,
    pub created_at: DateTime<Utc>,
}

impl Job {
    pub fn job_state(&self) -> Result<JobState, UnknownJobState> {
        JobState::from_str(&self.state)
    }
}
