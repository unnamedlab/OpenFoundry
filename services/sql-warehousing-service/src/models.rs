use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// Persistent SQL warehousing job: large-scale SQL execution against the warehouse with
/// intermediate persistence guarantees and lineage back to source datasets.
#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct WarehouseJob {
    pub id: Uuid,
    pub slug: String,
    pub sql_text: String,
    /// One of: queued, running, succeeded, failed, cancelled
    pub status: String,
    pub source_datasets: serde_json::Value,
    pub target_dataset_id: Option<Uuid>,
    pub target_storage_id: Option<Uuid>,
    pub submitted_by: Option<Uuid>,
    pub error_message: Option<String>,
    pub started_at: Option<DateTime<Utc>>,
    pub finished_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SubmitWarehouseJobRequest {
    pub slug: String,
    pub sql_text: String,
    #[serde(default)]
    pub source_datasets: Vec<Uuid>,
    pub target_dataset_id: Option<Uuid>,
    pub target_storage_id: Option<Uuid>,
}

/// Reusable SQL transformation declaration (templated SQL with bindings).
#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct WarehouseTransformation {
    pub id: Uuid,
    pub slug: String,
    pub description: Option<String>,
    pub sql_template: String,
    pub bindings: serde_json::Value,
    /// One of: draft, published, deprecated
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RegisterTransformationRequest {
    pub slug: String,
    pub description: Option<String>,
    pub sql_template: String,
    #[serde(default)]
    pub bindings: serde_json::Value,
}

/// Intermediate storage artifact produced by a warehouse job (parquet, table, materialized view).
#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct WarehouseStorageArtifact {
    pub id: Uuid,
    pub job_id: Option<Uuid>,
    pub slug: String,
    /// One of: parquet, table, materialized_view, temp
    pub artifact_kind: String,
    pub storage_uri: String,
    pub byte_size: Option<i64>,
    pub row_count: Option<i64>,
    /// One of: active, expired, evicted
    pub status: String,
    pub expires_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}
