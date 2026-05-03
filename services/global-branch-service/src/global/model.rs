use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct GlobalBranch {
    pub id: Uuid,
    pub rid: String,
    pub name: String,
    pub parent_global_branch: Option<Uuid>,
    pub description: String,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub archived_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct GlobalBranchLink {
    pub global_branch_id: Uuid,
    pub resource_type: String,
    pub resource_rid: String,
    pub branch_rid: String,
    pub status: String,
    pub last_synced_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateGlobalBranchRequest {
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub parent_global_branch: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct CreateGlobalBranchLinkRequest {
    pub resource_type: String,
    pub resource_rid: String,
    pub branch_rid: String,
}

#[derive(Debug, Serialize)]
pub struct GlobalBranchSummary {
    #[serde(flatten)]
    pub branch: GlobalBranch,
    pub link_count: i64,
    pub drifted_count: i64,
    pub archived_count: i64,
}

#[derive(Debug, Serialize)]
pub struct PromoteResponse {
    pub event_id: Uuid,
    pub global_branch_id: Uuid,
    pub topic: String,
}
