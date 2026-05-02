//! Stream branching primitives (Bloque E1).

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamBranch {
    pub id: Uuid,
    pub stream_id: Uuid,
    pub name: String,
    pub parent_branch_id: Option<Uuid>,
    pub status: String,
    pub head_sequence_no: i64,
    pub dataset_branch_id: Option<String>,
    pub description: String,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub archived_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateBranchRequest {
    pub name: String,
    pub parent_branch_id: Option<Uuid>,
    #[serde(default)]
    pub description: Option<String>,
    /// Optional reference to an existing dataset-versioning branch the
    /// cold tier should write to when this branch is archived.
    #[serde(default)]
    pub dataset_branch_id: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct MergeBranchRequest {
    /// Target branch (defaults to "main"). The merge bumps the target's
    /// `head_sequence_no` to `max(target.head, source.head)` and marks
    /// the source as `merged`.
    #[serde(default)]
    pub target_branch: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct MergeBranchResponse {
    pub source_branch_id: Uuid,
    pub target_branch_id: Uuid,
    pub merged_sequence_no: i64,
    pub message: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ArchiveBranchRequest {
    /// When true the dataset-versioning service is asked to commit the
    /// cold-tier copy of this branch. The handler emits a best-effort
    /// HTTP call; failures only surface in the response message.
    #[serde(default)]
    pub commit_cold: bool,
}

#[derive(Debug, Clone, FromRow)]
pub struct StreamBranchRow {
    pub id: Uuid,
    pub stream_id: Uuid,
    pub name: String,
    pub parent_branch_id: Option<Uuid>,
    pub status: String,
    pub head_sequence_no: i64,
    pub dataset_branch_id: Option<String>,
    pub description: String,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub archived_at: Option<DateTime<Utc>>,
}

impl From<StreamBranchRow> for StreamBranch {
    fn from(value: StreamBranchRow) -> Self {
        Self {
            id: value.id,
            stream_id: value.stream_id,
            name: value.name,
            parent_branch_id: value.parent_branch_id,
            status: value.status,
            head_sequence_no: value.head_sequence_no,
            dataset_branch_id: value.dataset_branch_id,
            description: value.description,
            created_by: value.created_by,
            created_at: value.created_at,
            archived_at: value.archived_at,
        }
    }
}
