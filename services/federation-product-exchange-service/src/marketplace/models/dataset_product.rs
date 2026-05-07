//! P5 — Foundry "Marketplace dataset products" model.
//!
//! Mirrors `migrations/20260503000003_dataset_products.sql`. Each
//! product captures the dataset's metadata snapshot the user opted
//! into when publishing (schema, branches, retention, schedules),
//! plus the source dataset RID + entity_type + version. Installing
//! creates a row in `marketplace_dataset_product_installs` keyed by
//! the target project and the target dataset RID.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetProductRow {
    pub id: Uuid,
    pub name: String,
    pub source_dataset_rid: String,
    pub entity_type: String,
    pub version: String,
    pub project_id: Option<Uuid>,
    pub published_by: Option<Uuid>,
    pub export_includes_data: bool,
    pub include_schema: bool,
    pub include_branches: bool,
    pub include_retention: bool,
    pub include_schedules: bool,
    pub manifest: Value,
    pub bootstrap_mode: String,
    pub published_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetProductInstallRow {
    pub id: Uuid,
    pub product_id: Uuid,
    pub target_project_id: Uuid,
    pub target_dataset_rid: String,
    pub bootstrap_mode: String,
    pub status: String,
    pub details: Value,
    pub installed_by: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
}

/// Per-product manifest. Stored verbatim in the JSONB column so the
/// install path can replay it without joining other tables (the
/// dataset's source might have moved between publish and install).
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct DatasetProductManifest {
    pub entity: String,
    pub version: String,
    /// Schema snapshot from the current view at publish time.
    /// `None` when `include_schema = false`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub schema: Option<Value>,
    /// Retention policies snapshot. List of explicit policies
    /// applicable to the dataset at publish time. Empty when
    /// `include_retention = false`.
    #[serde(default)]
    pub retention: Vec<Value>,
    /// Branching policy snapshot (default branch, parent map).
    /// `None` when `include_branches = false`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub branching_policy: Option<Value>,
    /// Pipeline schedule IDs upstream. Empty when
    /// `include_schedules = false`.
    #[serde(default)]
    pub schedules: Vec<String>,
    /// Bootstrap descriptor: `{ "mode": "schema-only" | "with-snapshot" }`.
    /// Mirrors `DatasetProductRow::bootstrap_mode` for self-contained
    /// downstream consumption.
    pub bootstrap: DatasetProductBootstrap,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct DatasetProductBootstrap {
    pub mode: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetProduct {
    pub id: Uuid,
    pub name: String,
    pub source_dataset_rid: String,
    pub entity_type: String,
    pub version: String,
    pub project_id: Option<Uuid>,
    pub published_by: Option<Uuid>,
    pub export_includes_data: bool,
    pub include_schema: bool,
    pub include_branches: bool,
    pub include_retention: bool,
    pub include_schedules: bool,
    pub manifest: DatasetProductManifest,
    pub bootstrap_mode: String,
    pub published_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
}

impl TryFrom<DatasetProductRow> for DatasetProduct {
    type Error = String;
    fn try_from(row: DatasetProductRow) -> Result<Self, Self::Error> {
        let manifest: DatasetProductManifest = serde_json::from_value(row.manifest.clone())
            .map_err(|e| format!("manifest decode: {e}"))?;
        Ok(Self {
            id: row.id,
            name: row.name,
            source_dataset_rid: row.source_dataset_rid,
            entity_type: row.entity_type,
            version: row.version,
            project_id: row.project_id,
            published_by: row.published_by,
            export_includes_data: row.export_includes_data,
            include_schema: row.include_schema,
            include_branches: row.include_branches,
            include_retention: row.include_retention,
            include_schedules: row.include_schedules,
            manifest,
            bootstrap_mode: row.bootstrap_mode,
            published_at: row.published_at,
            created_at: row.created_at,
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetProductInstall {
    pub id: Uuid,
    pub product_id: Uuid,
    pub target_project_id: Uuid,
    pub target_dataset_rid: String,
    pub bootstrap_mode: String,
    pub status: String,
    pub details: Value,
    pub installed_by: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
}

impl From<DatasetProductInstallRow> for DatasetProductInstall {
    fn from(row: DatasetProductInstallRow) -> Self {
        Self {
            id: row.id,
            product_id: row.product_id,
            target_project_id: row.target_project_id,
            target_dataset_rid: row.target_dataset_rid,
            bootstrap_mode: row.bootstrap_mode,
            status: row.status,
            details: row.details,
            installed_by: row.installed_by,
            created_at: row.created_at,
            completed_at: row.completed_at,
        }
    }
}

/// Body of `POST /v1/products/from-dataset/{rid}`.
#[derive(Debug, Clone, Deserialize)]
pub struct CreateDatasetProductRequest {
    pub name: String,
    #[serde(default = "default_version")]
    pub version: String,
    #[serde(default)]
    pub project_id: Option<Uuid>,
    #[serde(default)]
    pub published_by: Option<Uuid>,
    #[serde(default)]
    pub export_includes_data: bool,
    #[serde(default = "default_true")]
    pub include_schema: bool,
    #[serde(default)]
    pub include_branches: bool,
    #[serde(default)]
    pub include_retention: bool,
    #[serde(default)]
    pub include_schedules: bool,
    #[serde(default = "default_bootstrap_mode")]
    pub bootstrap_mode: String,

    /// Inline manifest fragments. The handler honours each fragment
    /// only when its matching `include_*` flag is also true. Allows
    /// the UI to pre-fetch the snapshot from neighbouring services
    /// (data-asset-catalog for schema, retention-policy-service for
    /// retention) and feed it straight in.
    #[serde(default)]
    pub schema: Option<Value>,
    #[serde(default)]
    pub retention: Vec<Value>,
    #[serde(default)]
    pub branching_policy: Option<Value>,
    #[serde(default)]
    pub schedules: Vec<String>,
}

fn default_version() -> String {
    "1.0.0".into()
}
fn default_true() -> bool {
    true
}
fn default_bootstrap_mode() -> String {
    "schema-only".into()
}

/// Body of `POST /v1/products/{id}/install`.
#[derive(Debug, Clone, Deserialize)]
pub struct InstallDatasetProductRequest {
    pub target_project_id: Uuid,
    pub target_dataset_rid: String,
    /// Optional override; when omitted the product's published
    /// `bootstrap_mode` wins.
    #[serde(default)]
    pub bootstrap_mode: Option<String>,
    #[serde(default)]
    pub installed_by: Option<Uuid>,
}
