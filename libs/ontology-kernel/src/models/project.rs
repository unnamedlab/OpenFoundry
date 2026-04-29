use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Copy, Serialize, Deserialize, sqlx::Type, PartialEq, Eq)]
#[sqlx(type_name = "text", rename_all = "snake_case")]
#[serde(rename_all = "snake_case")]
pub enum OntologyProjectRole {
    Viewer,
    Editor,
    Owner,
}

impl OntologyProjectRole {
    pub fn rank(self) -> u8 {
        match self {
            Self::Viewer => 1,
            Self::Editor => 2,
            Self::Owner => 3,
        }
    }
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct OntologyProject {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    pub description: String,
    pub workspace_slug: Option<String>,
    pub owner_id: Uuid,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct OntologyProjectMembership {
    pub project_id: Uuid,
    pub user_id: Uuid,
    pub role: OntologyProjectRole,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct OntologyProjectResourceBinding {
    pub project_id: Uuid,
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub bound_by: Uuid,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateOntologyProjectRequest {
    pub slug: String,
    pub display_name: Option<String>,
    pub description: Option<String>,
    pub workspace_slug: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateOntologyProjectRequest {
    pub display_name: Option<String>,
    pub description: Option<String>,
    pub workspace_slug: Option<Option<String>>,
}

#[derive(Debug, Deserialize)]
pub struct ListOntologyProjectsQuery {
    pub search: Option<String>,
    pub page: Option<i64>,
    pub per_page: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct ListOntologyProjectsResponse {
    pub data: Vec<OntologyProject>,
    pub total: i64,
    pub page: i64,
    pub per_page: i64,
}

#[derive(Debug, Deserialize)]
pub struct UpsertOntologyProjectMembershipRequest {
    pub user_id: Uuid,
    pub role: OntologyProjectRole,
}

#[derive(Debug, Serialize)]
pub struct ListOntologyProjectMembershipsResponse {
    pub data: Vec<OntologyProjectMembership>,
}

#[derive(Debug, Deserialize)]
pub struct BindOntologyProjectResourceRequest {
    pub resource_kind: String,
    pub resource_id: Uuid,
}

#[derive(Debug, Serialize)]
pub struct ListOntologyProjectResourcesResponse {
    pub data: Vec<OntologyProjectResourceBinding>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OntologyProjectWorkingState {
    pub project_id: Uuid,
    pub changes: Value,
    pub updated_by: Uuid,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct ReplaceOntologyProjectWorkingStateRequest {
    pub changes: Value,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct OntologyProjectBranch {
    pub id: Uuid,
    pub project_id: Uuid,
    pub name: String,
    pub description: String,
    pub status: String,
    pub proposal_id: Option<Uuid>,
    pub changes: Value,
    pub conflict_resolutions: Value,
    pub enable_indexing: bool,
    pub created_by: Uuid,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub latest_rebased_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateOntologyProjectBranchRequest {
    pub name: String,
    pub description: Option<String>,
    pub changes: Value,
    pub enable_indexing: Option<bool>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateOntologyProjectBranchRequest {
    pub description: Option<String>,
    pub status: Option<String>,
    pub proposal_id: Option<Option<Uuid>>,
    pub changes: Option<Value>,
    pub conflict_resolutions: Option<Value>,
    pub enable_indexing: Option<bool>,
    pub latest_rebased_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Serialize)]
pub struct ListOntologyProjectBranchesResponse {
    pub data: Vec<OntologyProjectBranch>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct OntologyProjectProposal {
    pub id: Uuid,
    pub project_id: Uuid,
    pub branch_id: Uuid,
    pub title: String,
    pub description: String,
    pub status: String,
    pub reviewer_ids: Value,
    pub tasks: Value,
    pub comments: Value,
    pub created_by: Uuid,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateOntologyProjectProposalRequest {
    pub branch_id: Uuid,
    pub title: String,
    pub description: Option<String>,
    pub reviewer_ids: Option<Value>,
    pub tasks: Value,
    pub comments: Option<Value>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateOntologyProjectProposalRequest {
    pub title: Option<String>,
    pub description: Option<String>,
    pub status: Option<String>,
    pub reviewer_ids: Option<Value>,
    pub tasks: Option<Value>,
    pub comments: Option<Value>,
}

#[derive(Debug, Serialize)]
pub struct ListOntologyProjectProposalsResponse {
    pub data: Vec<OntologyProjectProposal>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct OntologyProjectMigration {
    pub id: Uuid,
    pub project_id: Uuid,
    pub source_project_id: Uuid,
    pub target_project_id: Uuid,
    pub resources: Value,
    pub submitted_at: DateTime<Utc>,
    pub status: String,
    pub note: String,
    pub submitted_by: Uuid,
}

#[derive(Debug, Deserialize)]
pub struct CreateOntologyProjectMigrationRequest {
    pub source_project_id: Uuid,
    pub target_project_id: Uuid,
    pub resources: Value,
    pub note: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ListOntologyProjectMigrationsResponse {
    pub data: Vec<OntologyProjectMigration>,
}
