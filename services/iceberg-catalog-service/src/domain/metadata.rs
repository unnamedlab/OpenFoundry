//! Iceberg `metadata.json` builder and parser (spec format v2).
//!
//! The catalog persists table state in Postgres for queryability, but
//! external Iceberg clients (PyIceberg, Spark, Trino, …) consume the
//! canonical JSON metadata file from object storage. This module is the
//! single source of truth for that translation.
//!
//! References:
//!   * <https://iceberg.apache.org/spec/#table-metadata>
//!   * <https://github.com/apache/iceberg/blob/main/open-api/rest-catalog-open-api.yaml>

use chrono::Utc;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use super::snapshot::Snapshot;
use super::table::IcebergTable;

/// Output of [`build_metadata_v2`]. Newtype around `serde_json::Value`
/// so callers can't confuse it with a generic JSON blob at the type
/// system level.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TableMetadataDocument(pub Value);

impl TableMetadataDocument {
    pub fn as_value(&self) -> &Value {
        &self.0
    }

    pub fn into_value(self) -> Value {
        self.0
    }

    pub fn to_pretty_string(&self) -> String {
        serde_json::to_string_pretty(&self.0).unwrap_or_default()
    }
}

/// Parsed table metadata produced by [`parse_metadata`].
///
/// Only the fields the catalog itself needs to make decisions are
/// surfaced; the full document is preserved verbatim in [`Self::document`]
/// so we never round-trip-modify metadata produced by another writer.
#[derive(Debug, Clone)]
pub struct TableMetadata {
    pub format_version: i32,
    pub table_uuid: String,
    pub location: String,
    pub last_sequence_number: i64,
    pub current_snapshot_id: Option<i64>,
    pub document: Value,
}

#[derive(Debug, thiserror::Error)]
pub enum MetadataError {
    #[error("required field `{0}` missing or wrong type")]
    Missing(&'static str),
    #[error("unsupported format-version {0} (catalog accepts 1, 2, 3)")]
    UnsupportedFormatVersion(i64),
    #[error("invalid metadata json: {0}")]
    Invalid(String),
}

/// Build a v2 `metadata.json` document for `table` and the supplied
/// `snapshots` (ordered chronologically, oldest first). The output is
/// intentionally close to the on-disk format produced by Spark and
/// PyIceberg writers so external clients have nothing to special-case.
pub fn build_metadata_v2(table: &IcebergTable, snapshots: &[Snapshot]) -> TableMetadataDocument {
    let now_ms = Utc::now().timestamp_millis();
    let snapshots_json: Vec<Value> = snapshots
        .iter()
        .map(|s| {
            json!({
                "snapshot-id": s.snapshot_id,
                "parent-snapshot-id": s.parent_snapshot_id,
                "sequence-number": s.sequence_number,
                "timestamp-ms": s.timestamp_ms,
                "summary": merge_operation_into_summary(&s.summary, &s.operation),
                "manifest-list": s.manifest_list_location,
                "schema-id": s.schema_id,
            })
        })
        .collect();

    let snapshot_log: Vec<Value> = snapshots
        .iter()
        .map(|s| {
            json!({
                "timestamp-ms": s.timestamp_ms,
                "snapshot-id": s.snapshot_id,
            })
        })
        .collect();

    let metadata_log: Vec<Value> = vec![json!({
        "timestamp-ms": now_ms,
        "metadata-file": format!("{}/metadata/v{}.metadata.json", table.location, table.format_version),
    })];

    // Iceberg expects a `refs` map even when the only ref is `main`.
    let mut refs = serde_json::Map::new();
    if let Some(snapshot_id) = table.current_snapshot_id {
        refs.insert(
            "main".to_string(),
            json!({
                "snapshot-id": snapshot_id,
                "type": "branch",
            }),
        );
    }

    let doc = json!({
        "format-version": table.format_version,
        "table-uuid": table.table_uuid,
        "location": table.location,
        "last-sequence-number": table.last_sequence_number,
        "last-updated-ms": now_ms,
        "last-column-id": last_column_id(&table.schema_json),
        "current-schema-id": current_schema_id(&table.schema_json),
        "schemas": [&table.schema_json],
        "default-spec-id": 0,
        "partition-specs": [&table.partition_spec],
        "last-partition-id": 1000,
        "default-sort-order-id": 0,
        "sort-orders": [&table.sort_order],
        "properties": &table.properties,
        "current-snapshot-id": table.current_snapshot_id.unwrap_or(-1),
        "refs": refs,
        "snapshots": snapshots_json,
        "snapshot-log": snapshot_log,
        "metadata-log": metadata_log,
    });

    TableMetadataDocument(doc)
}

/// Parse a `metadata.json` document into a [`TableMetadata`] handle.
pub fn parse_metadata(json: &Value) -> Result<TableMetadata, MetadataError> {
    let format_version = json
        .get("format-version")
        .and_then(Value::as_i64)
        .ok_or(MetadataError::Missing("format-version"))?;
    if !(1..=3).contains(&format_version) {
        return Err(MetadataError::UnsupportedFormatVersion(format_version));
    }
    let table_uuid = json
        .get("table-uuid")
        .and_then(Value::as_str)
        .ok_or(MetadataError::Missing("table-uuid"))?
        .to_string();
    let location = json
        .get("location")
        .and_then(Value::as_str)
        .ok_or(MetadataError::Missing("location"))?
        .to_string();
    let last_sequence_number = json
        .get("last-sequence-number")
        .and_then(Value::as_i64)
        .unwrap_or(0);
    let current_snapshot_id = json
        .get("current-snapshot-id")
        .and_then(Value::as_i64)
        .filter(|id| *id >= 0);

    Ok(TableMetadata {
        format_version: format_version as i32,
        table_uuid,
        location,
        last_sequence_number,
        current_snapshot_id,
        document: json.clone(),
    })
}

fn current_schema_id(schema: &Value) -> i64 {
    schema
        .get("schema-id")
        .and_then(Value::as_i64)
        .unwrap_or(0)
}

fn last_column_id(schema: &Value) -> i64 {
    schema
        .get("fields")
        .and_then(Value::as_array)
        .map(|fields| fields.len() as i64)
        .unwrap_or(0)
}

/// Iceberg's `summary` always carries an `operation` key — handlers store
/// it in the `operation` column, so we re-merge it into the canonical
/// summary map when serialising.
fn merge_operation_into_summary(summary: &Value, operation: &str) -> Value {
    let mut map = summary
        .as_object()
        .cloned()
        .unwrap_or_else(serde_json::Map::new);
    map.insert("operation".to_string(), Value::String(operation.to_string()));
    Value::Object(map)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use uuid::Uuid;

    fn fixture_table() -> IcebergTable {
        IcebergTable {
            id: Uuid::nil(),
            rid: "ri.foundry.main.iceberg-table.0".to_string(),
            namespace_id: Uuid::nil(),
            name: "events".to_string(),
            table_uuid: "5b6c1f8e-3a01-4f02-8e1c-aa0d65cf6c2c".to_string(),
            format_version: 2,
            location: "s3://warehouse/events".to_string(),
            current_snapshot_id: Some(1),
            current_metadata_location: Some(
                "s3://warehouse/events/metadata/v1.metadata.json".to_string(),
            ),
            last_sequence_number: 1,
            partition_spec: json!({ "spec-id": 0, "fields": [] }),
            schema_json: json!({
                "schema-id": 0,
                "type": "struct",
                "fields": [
                    {"id": 1, "name": "id", "required": true, "type": "long"},
                    {"id": 2, "name": "name", "required": false, "type": "string"},
                ]
            }),
            sort_order: json!({ "order-id": 0, "fields": [] }),
            properties: json!({}),
            markings: vec!["public".to_string()],
            namespace_path: vec!["ns".to_string()],
        }
    }

    fn fixture_snapshot() -> Snapshot {
        Snapshot {
            id: 1,
            table_id: Uuid::nil(),
            snapshot_id: 1,
            parent_snapshot_id: None,
            sequence_number: 1,
            operation: "append".to_string(),
            manifest_list_location: "s3://warehouse/events/metadata/snap-1-manifest-list.avro"
                .to_string(),
            summary: json!({ "added-data-files": "1", "added-records": "10" }),
            schema_id: 0,
            timestamp_ms: 1_700_000_000_000,
        }
    }

    #[test]
    fn metadata_v2_round_trips_through_parse() {
        let table = fixture_table();
        let snapshots = vec![fixture_snapshot()];
        let doc = build_metadata_v2(&table, &snapshots).into_value();
        let parsed = parse_metadata(&doc).expect("parse should succeed");

        assert_eq!(parsed.format_version, 2);
        assert_eq!(parsed.table_uuid, table.table_uuid);
        assert_eq!(parsed.location, table.location);
        assert_eq!(parsed.current_snapshot_id, Some(1));
        assert_eq!(parsed.last_sequence_number, 1);
    }

    #[test]
    fn snapshot_summary_includes_operation() {
        let table = fixture_table();
        let snapshots = vec![fixture_snapshot()];
        let doc = build_metadata_v2(&table, &snapshots).into_value();
        let summary = doc["snapshots"][0]["summary"].clone();
        assert_eq!(summary["operation"], json!("append"));
        assert_eq!(summary["added-records"], json!("10"));
    }

    #[test]
    fn refs_map_carries_main_branch_pointing_at_current_snapshot() {
        let table = fixture_table();
        let snapshots = vec![fixture_snapshot()];
        let doc = build_metadata_v2(&table, &snapshots).into_value();
        assert_eq!(doc["refs"]["main"]["snapshot-id"], json!(1));
        assert_eq!(doc["refs"]["main"]["type"], json!("branch"));
    }

    #[test]
    fn rejects_unsupported_format_version() {
        let bad = json!({
            "format-version": 4,
            "table-uuid": "x",
            "location": "s3://x"
        });
        let err = parse_metadata(&bad).unwrap_err();
        assert!(matches!(err, MetadataError::UnsupportedFormatVersion(4)));
    }
}
