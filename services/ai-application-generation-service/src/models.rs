use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct AppGenerationSeed {
    pub id: Uuid,
    pub slug: String,
    pub name: String,
    pub description: Option<String>,
    pub template: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateSeedRequest {
    pub slug: String,
    pub name: String,
    pub description: Option<String>,
    pub template: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct AppGenerationSession {
    pub id: Uuid,
    pub seed_id: Option<Uuid>,
    pub goal: String,
    pub status: String,
    pub context: serde_json::Value,
    pub generated_app_id: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct StartSessionRequest {
    pub seed_id: Option<Uuid>,
    pub goal: String,
    pub context: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct GenerateRequest {
    pub instructions: Option<String>,
    pub overrides: Option<serde_json::Value>,
}
