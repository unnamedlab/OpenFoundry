use chrono::{DateTime, Utc};
use core_models::security::EffectiveMarking;
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

use crate::models::{
    branch::DatasetBranch, dataset::Dataset, schema::DatasetSchema, version::DatasetVersion,
    view::DatasetView,
};

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetPermissionEdge {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub principal_kind: String,
    pub principal_id: String,
    pub role: String,
    pub actions: Vec<String>,
    pub source: String,
    pub inherited_from: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetLineageLink {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub direction: String,
    pub target_rid: String,
    pub target_kind: String,
    pub relation_kind: String,
    pub pipeline_id: Option<String>,
    pub workflow_id: Option<String>,
    pub metadata: serde_json::Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetFileIndexEntry {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub path: String,
    pub storage_path: String,
    pub entry_type: String,
    pub size_bytes: i64,
    pub content_type: Option<String>,
    pub metadata: serde_json::Value,
    pub last_modified: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetHealthSummary {
    pub status: String,
    pub quality_score: Option<f64>,
    pub profile_generated_at: Option<String>,
    pub active_alert_count: i64,
    pub lint_posture: Option<String>,
    pub lint_finding_count: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetRichModel {
    #[serde(flatten)]
    pub dataset: Dataset,
    pub schema: Option<DatasetSchema>,
    pub files: Vec<DatasetFileIndexEntry>,
    pub branches: Vec<DatasetBranch>,
    pub versions: Vec<DatasetVersion>,
    pub current_view: Option<DatasetView>,
    pub health: DatasetHealthSummary,
    pub markings: Vec<EffectiveMarking>,
    pub permissions: Vec<DatasetPermissionEdge>,
    pub lineage_links: Vec<DatasetLineageLink>,
}

#[derive(Debug, Deserialize)]
pub struct DatasetMetadataPatch {
    pub name: Option<String>,
    pub description: Option<String>,
    pub owner_id: Option<Uuid>,
    pub tags: Option<Vec<String>>,
    pub format: Option<String>,
    pub metadata: Option<serde_json::Value>,
    pub schema: Option<serde_json::Value>,
    pub health_status: Option<String>,
    pub current_view_id: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetMarkingsRequest {
    pub markings: Vec<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetPermissionsRequest {
    pub permissions: Vec<PutDatasetPermissionEdge>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetPermissionEdge {
    pub principal_kind: String,
    pub principal_id: String,
    pub role: String,
    pub actions: Vec<String>,
    pub source: Option<String>,
    pub inherited_from: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetLineageLinksRequest {
    pub links: Vec<PutDatasetLineageLink>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetLineageLink {
    pub direction: String,
    pub target_rid: String,
    pub target_kind: Option<String>,
    pub relation_kind: Option<String>,
    pub pipeline_id: Option<String>,
    pub workflow_id: Option<String>,
    pub metadata: Option<serde_json::Value>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetFilesRequest {
    pub files: Vec<PutDatasetFileIndexEntry>,
}

#[derive(Debug, Deserialize)]
pub struct PutDatasetFileIndexEntry {
    pub path: String,
    pub storage_path: String,
    pub entry_type: Option<String>,
    pub size_bytes: Option<i64>,
    pub content_type: Option<String>,
    pub metadata: Option<serde_json::Value>,
    pub last_modified: Option<DateTime<Utc>>,
}
