use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct Repository {
    pub id: Uuid,
    pub name: String,
    pub default_branch: String,
    pub visibility: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateRepositoryRequest {
    pub name: String,
    pub default_branch: Option<String>,
    pub visibility: Option<String>,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct Commit {
    pub id: Uuid,
    pub repository_id: Uuid,
    pub sha: String,
    pub author: String,
    pub message: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateCommitRequest {
    pub sha: String,
    pub author: String,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct MergeRequest {
    pub id: Uuid,
    pub repository_id: Uuid,
    pub source_branch: String,
    pub target_branch: String,
    pub title: String,
    pub status: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateMergeRequestRequest {
    pub source_branch: String,
    pub target_branch: String,
    pub title: String,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct ReviewComment {
    pub id: Uuid,
    pub merge_request_id: Uuid,
    pub author: String,
    pub body: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateReviewCommentRequest {
    pub author: String,
    pub body: String,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct CiRun {
    pub id: Uuid,
    pub repository_id: Uuid,
    pub commit_sha: String,
    pub status: String,
    pub started_at: Option<DateTime<Utc>>,
    pub finished_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateCiRunRequest {
    pub commit_sha: String,
}
