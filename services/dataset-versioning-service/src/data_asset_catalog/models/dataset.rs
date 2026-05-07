use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct Dataset {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub format: String,
    pub storage_path: String,
    pub size_bytes: i64,
    pub row_count: i64,
    pub owner_id: Uuid,
    pub tags: Vec<String>,
    pub current_version: i32,
    pub active_branch: String,
    pub metadata: serde_json::Value,
    pub health_status: String,
    pub current_view_id: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateDatasetRequest {
    pub name: String,
    pub description: Option<String>,
    pub format: Option<String>,
    pub tags: Option<Vec<String>>,
    pub metadata: Option<serde_json::Value>,
    pub health_status: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateDatasetRequest {
    pub name: Option<String>,
    pub description: Option<String>,
    pub owner_id: Option<Uuid>,
    pub tags: Option<Vec<String>>,
    pub metadata: Option<serde_json::Value>,
    pub health_status: Option<String>,
    pub current_view_id: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct ListDatasetsQuery {
    pub page: Option<i64>,
    pub per_page: Option<i64>,
    pub search: Option<String>,
    pub tag: Option<String>,
    pub owner_id: Option<Uuid>,
}
