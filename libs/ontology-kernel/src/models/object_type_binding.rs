use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

/// Sync mode for an `ObjectType`-to-dataset binding.
///
/// * `Snapshot` – every materialisation truncates the prior set of bound
///   instances and re-inserts the dataset rows.
/// * `Incremental` – upsert by primary key column (insert new, update existing).
/// * `View` – metadata-only binding; rows are not materialised, the read-side
///   is expected to serve them lazily from the dataset.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, sqlx::Type)]
#[sqlx(type_name = "text", rename_all = "snake_case")]
#[serde(rename_all = "snake_case")]
pub enum ObjectTypeBindingSyncMode {
    Snapshot,
    Incremental,
    View,
}

impl ObjectTypeBindingSyncMode {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Snapshot => "snapshot",
            Self::Incremental => "incremental",
            Self::View => "view",
        }
    }
}

impl TryFrom<&str> for ObjectTypeBindingSyncMode {
    type Error = String;

    fn try_from(value: &str) -> Result<Self, Self::Error> {
        match value.trim() {
            "snapshot" => Ok(Self::Snapshot),
            "incremental" => Ok(Self::Incremental),
            "view" => Ok(Self::View),
            other => Err(format!(
                "sync_mode '{other}' is not supported; expected one of: snapshot, incremental, view"
            )),
        }
    }
}

/// One source-to-property mapping inside a binding.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObjectTypeBindingPropertyMapping {
    /// Column name in the source dataset row.
    pub source_field: String,
    /// Target property name on the ObjectType.
    pub target_property: String,
}

/// Persisted shape of an `object_type_bindings` row.
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct ObjectTypeBindingRow {
    pub id: Uuid,
    pub object_type_id: Uuid,
    pub dataset_id: Uuid,
    pub dataset_branch: Option<String>,
    pub dataset_version: Option<i32>,
    pub primary_key_column: String,
    pub property_mapping: Value,
    pub sync_mode: String,
    pub default_marking: String,
    pub preview_limit: i32,
    pub owner_id: Uuid,
    pub last_materialized_at: Option<DateTime<Utc>>,
    pub last_run_status: Option<String>,
    pub last_run_summary: Option<Value>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// Public API representation of a binding.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObjectTypeBinding {
    pub id: Uuid,
    pub object_type_id: Uuid,
    pub dataset_id: Uuid,
    pub dataset_branch: Option<String>,
    pub dataset_version: Option<i32>,
    pub primary_key_column: String,
    pub property_mapping: Vec<ObjectTypeBindingPropertyMapping>,
    pub sync_mode: ObjectTypeBindingSyncMode,
    pub default_marking: String,
    pub preview_limit: i32,
    pub owner_id: Uuid,
    pub last_materialized_at: Option<DateTime<Utc>>,
    pub last_run_status: Option<String>,
    pub last_run_summary: Option<Value>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl TryFrom<ObjectTypeBindingRow> for ObjectTypeBinding {
    type Error = String;

    fn try_from(row: ObjectTypeBindingRow) -> Result<Self, Self::Error> {
        let property_mapping: Vec<ObjectTypeBindingPropertyMapping> =
            serde_json::from_value(row.property_mapping)
                .map_err(|error| format!("failed to decode property_mapping: {error}"))?;
        let sync_mode = ObjectTypeBindingSyncMode::try_from(row.sync_mode.as_str())?;
        Ok(Self {
            id: row.id,
            object_type_id: row.object_type_id,
            dataset_id: row.dataset_id,
            dataset_branch: row.dataset_branch,
            dataset_version: row.dataset_version,
            primary_key_column: row.primary_key_column,
            property_mapping,
            sync_mode,
            default_marking: row.default_marking,
            preview_limit: row.preview_limit,
            owner_id: row.owner_id,
            last_materialized_at: row.last_materialized_at,
            last_run_status: row.last_run_status,
            last_run_summary: row.last_run_summary,
            created_at: row.created_at,
            updated_at: row.updated_at,
        })
    }
}

#[derive(Debug, Deserialize)]
pub struct CreateObjectTypeBindingRequest {
    pub dataset_id: Uuid,
    pub dataset_branch: Option<String>,
    pub dataset_version: Option<i32>,
    pub primary_key_column: String,
    #[serde(default)]
    pub property_mapping: Vec<ObjectTypeBindingPropertyMapping>,
    pub sync_mode: ObjectTypeBindingSyncMode,
    pub default_marking: Option<String>,
    pub preview_limit: Option<i32>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateObjectTypeBindingRequest {
    pub dataset_branch: Option<String>,
    pub dataset_version: Option<i32>,
    pub primary_key_column: Option<String>,
    pub property_mapping: Option<Vec<ObjectTypeBindingPropertyMapping>>,
    pub sync_mode: Option<ObjectTypeBindingSyncMode>,
    pub default_marking: Option<String>,
    pub preview_limit: Option<i32>,
}

#[derive(Debug, Deserialize, Default)]
pub struct MaterializeBindingRequest {
    /// Optional override for the dataset branch to read.
    pub dataset_branch: Option<String>,
    /// Optional override for the dataset version to read.
    pub dataset_version: Option<i32>,
    /// Optional override for the per-run row limit. Capped at the binding's
    /// configured `preview_limit` for safety.
    pub limit: Option<i32>,
    /// When `true` no rows are written; only counts and validation errors are
    /// reported.
    #[serde(default)]
    pub dry_run: bool,
}

#[derive(Debug, Serialize)]
pub struct MaterializeBindingResponse {
    pub binding_id: Uuid,
    pub status: String,
    pub rows_read: i32,
    pub inserted: i32,
    pub updated: i32,
    pub skipped: i32,
    pub errors: i32,
    pub dry_run: bool,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub error_details: Vec<Value>,
}

#[derive(Debug, Serialize)]
pub struct ListObjectTypeBindingsResponse {
    pub data: Vec<ObjectTypeBinding>,
}
