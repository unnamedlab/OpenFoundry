use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct ModelAdapter {
    pub id: Uuid,
    pub slug: String,
    pub name: String,
    pub adapter_kind: String,
    pub artifact_uri: String,
    pub sidecar_image: Option<String>,
    pub framework: Option<String>,
    pub model_id: Option<Uuid>,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RegisterAdapterRequest {
    pub slug: String,
    pub name: String,
    pub adapter_kind: String,
    pub artifact_uri: String,
    pub sidecar_image: Option<String>,
    pub framework: Option<String>,
    pub model_id: Option<Uuid>,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct InferenceContract {
    pub id: Uuid,
    pub adapter_id: Uuid,
    pub version: String,
    pub input_schema: serde_json::Value,
    pub output_schema: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PublishContractRequest {
    pub version: String,
    pub input_schema: serde_json::Value,
    pub output_schema: serde_json::Value,
}
