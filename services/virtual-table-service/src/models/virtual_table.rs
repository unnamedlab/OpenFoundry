//! Persisted shapes for `virtual_tables`, `virtual_table_sources_link`
//! and `virtual_table_imports`.
//!
//! Schema lives in `migrations/20260504000120_virtual_tables_init.sql`.
//! The DTO surface lives next to the schema rather than next to the
//! handlers because both the gRPC server (proto-generated) and the
//! Axum handlers convert from these rows.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

use crate::domain::capability_matrix::{Capabilities, SourceProvider, TableType};

/// Row of `virtual_table_sources_link`.
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct VirtualTableSourceLink {
    pub source_rid: String,
    pub provider: String,
    pub virtual_tables_enabled: bool,
    pub code_imports_enabled: bool,
    pub export_controls: Value,
    pub auto_register_project_rid: Option<String>,
    pub auto_register_enabled: bool,
    pub auto_register_interval_seconds: Option<i32>,
    pub auto_register_tag_filters: Value,
    pub iceberg_catalog_kind: Option<String>,
    pub iceberg_catalog_config: Option<Value>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl VirtualTableSourceLink {
    pub fn provider_enum(&self) -> Option<SourceProvider> {
        SourceProvider::parse(&self.provider)
    }
}

/// Row of `virtual_tables`.
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct VirtualTableRow {
    pub id: Uuid,
    pub rid: String,
    pub source_rid: String,
    pub project_rid: String,
    pub name: String,
    pub parent_folder_rid: Option<String>,
    pub locator: Value,
    pub table_type: String,
    pub schema_inferred: Value,
    pub capabilities: Value,
    pub update_detection_enabled: bool,
    pub update_detection_interval_seconds: Option<i32>,
    pub last_observed_version: Option<String>,
    pub last_polled_at: Option<DateTime<Utc>>,
    /// P5 — exponential-backoff bookkeeping for the update-detection
    /// poller (see migration `20260504000122_update_detection.sql`).
    /// Default `0` so existing rows stay polite.
    #[serde(default)]
    pub update_detection_consecutive_failures: i32,
    /// P5 — when the poller is allowed to probe this row again.
    /// `NULL` means "ASAP" (the default for newly-enabled rows).
    pub update_detection_next_poll_at: Option<DateTime<Utc>>,
    pub markings: Vec<String>,
    pub properties: Value,
    pub created_by: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl VirtualTableRow {
    pub fn table_type_enum(&self) -> Option<TableType> {
        TableType::parse(&self.table_type)
    }

    pub fn capabilities_typed(&self) -> Option<Capabilities> {
        serde_json::from_value(self.capabilities.clone()).ok()
    }
}

/// API-facing locator. The DB column is `JSONB` — we serialize via
/// `Locator::canonicalize` so that the unique index on
/// `(source_rid, locator)` collapses semantically equivalent shapes
/// (key-order independent).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Locator {
    Tabular {
        database: String,
        schema: String,
        table: String,
    },
    File {
        bucket: String,
        prefix: String,
        format: String,
    },
    Iceberg {
        catalog: String,
        namespace: String,
        table: String,
    },
}

impl Locator {
    /// Stable canonical JSON form used for the unique index. Keys are
    /// sorted, fields trimmed.
    pub fn canonicalize(&self) -> Value {
        match self {
            Locator::Tabular {
                database,
                schema,
                table,
            } => serde_json::json!({
                "kind": "tabular",
                "database": database.trim(),
                "schema": schema.trim(),
                "table": table.trim(),
            }),
            Locator::File {
                bucket,
                prefix,
                format,
            } => serde_json::json!({
                "kind": "file",
                "bucket": bucket.trim(),
                "prefix": prefix.trim(),
                "format": format.trim().to_lowercase(),
            }),
            Locator::Iceberg {
                catalog,
                namespace,
                table,
            } => serde_json::json!({
                "kind": "iceberg",
                "catalog": catalog.trim(),
                "namespace": namespace.trim(),
                "table": table.trim(),
            }),
        }
    }

    /// Display name used as the default Foundry resource name when the
    /// caller does not provide one.
    pub fn default_display_name(&self) -> String {
        match self {
            Locator::Tabular {
                schema: _, table, ..
            } => table.clone(),
            Locator::File { bucket, prefix, .. } => {
                if prefix.is_empty() {
                    bucket.clone()
                } else {
                    format!("{bucket}/{prefix}")
                }
            }
            Locator::Iceberg { table, .. } => table.clone(),
        }
    }
}

/// Body of `POST /v1/sources/{source_rid}/virtual-tables/register`.
#[derive(Debug, Clone, Deserialize)]
pub struct RegisterVirtualTableRequest {
    pub project_rid: String,
    pub name: Option<String>,
    pub parent_folder_rid: Option<String>,
    pub locator: Locator,
    pub table_type: String,
    #[serde(default)]
    pub markings: Vec<String>,
}

/// Body of `POST /v1/sources/{source_rid}/virtual-tables/bulk-register`.
#[derive(Debug, Clone, Deserialize)]
pub struct BulkRegisterRequest {
    pub project_rid: String,
    pub entries: Vec<RegisterVirtualTableRequest>,
}

#[derive(Debug, Clone, Serialize)]
pub struct BulkRegisterResponse {
    pub registered: Vec<VirtualTableRow>,
    pub errors: Vec<BulkRegisterError>,
}

#[derive(Debug, Clone, Serialize)]
pub struct BulkRegisterError {
    pub name: String,
    pub error: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateMarkingsRequest {
    pub markings: Vec<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct EnableSourceRequest {
    pub provider: String,
    #[serde(default)]
    pub iceberg_catalog_kind: Option<String>,
    #[serde(default)]
    pub iceberg_catalog_config: Option<Value>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DiscoverQuery {
    #[serde(default)]
    pub path: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DiscoveredEntry {
    pub display_name: String,
    pub path: String,
    pub kind: String,
    pub registrable: bool,
    pub inferred_table_type: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ListVirtualTablesQuery {
    #[serde(default)]
    pub project: Option<String>,
    #[serde(default)]
    pub source: Option<String>,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default, rename = "type")]
    pub table_type: Option<String>,
    #[serde(default)]
    pub limit: Option<i64>,
    #[serde(default)]
    pub cursor: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ListVirtualTablesResponse {
    pub items: Vec<VirtualTableRow>,
    pub next_cursor: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn locator_canonicalize_is_key_order_independent() {
        let a = Locator::Tabular {
            database: " db ".to_string(),
            schema: "s".to_string(),
            table: "t".to_string(),
        };
        let b: Locator = serde_json::from_value(serde_json::json!({
            "kind": "tabular",
            "table": "t",
            "schema": "s",
            "database": "db",
        }))
        .expect("decode");
        assert_eq!(a.canonicalize(), b.canonicalize());
    }

    #[test]
    fn file_locator_lowercases_format() {
        let l = Locator::File {
            bucket: "b".into(),
            prefix: "p".into(),
            format: "PARQUET".into(),
        };
        assert_eq!(l.canonicalize()["format"], "parquet");
    }

    #[test]
    fn default_display_name_picks_table_segment() {
        let l = Locator::Tabular {
            database: "warehouse".into(),
            schema: "public".into(),
            table: "orders".into(),
        };
        assert_eq!(l.default_display_name(), "orders");
    }
}
