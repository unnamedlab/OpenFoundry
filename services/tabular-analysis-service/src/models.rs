use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct TabularAnalysisJob {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub analysis_kind: String,
    pub status: String,
    pub options: serde_json::Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SubmitJobRequest {
    pub dataset_id: Uuid,
    pub analysis_kind: String,
    pub options: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct TabularAnalysisResult {
    pub id: Uuid,
    pub job_id: Uuid,
    pub result_kind: String,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PublishResultRequest {
    pub result_kind: String,
    pub payload: serde_json::Value,
}
