use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct ModelSubmission {
    pub id: Uuid,
    pub model_id: Uuid,
    pub version: String,
    pub stage: String,
    pub status: String,
    pub objective_id: Option<Uuid>,
    pub release_notes: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateSubmissionRequest {
    pub model_id: Uuid,
    pub version: String,
    pub objective_id: Option<Uuid>,
    pub release_notes: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct TransitionRequest {
    pub stage: String,
    pub status: String,
    pub note: Option<String>,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct ModelingObjective {
    pub id: Uuid,
    pub slug: String,
    pub name: String,
    pub description: Option<String>,
    pub success_criteria: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateObjectiveRequest {
    pub slug: String,
    pub name: String,
    pub description: Option<String>,
    pub success_criteria: serde_json::Value,
}
