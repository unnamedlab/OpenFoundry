use serde::Serialize;
use serde_json::Value;

use sha2::{Digest, Sha256};

use std::sync::Arc;

use arrow_array::{ArrayRef, RecordBatch, StringArray};
use arrow_ipc::writer::StreamWriter;
use arrow_schema::{DataType, Field, Schema};

use crate::models::registration::{DiscoveredSource, VirtualTableQueryResponse};

pub mod azure_blob;
pub mod bigquery;
mod catalog_bridge;
pub mod csv;
pub mod databricks;
pub mod gcs;
pub mod generic;
pub mod http_runtime;
pub mod iot;
pub mod jdbc;
pub mod json;
pub mod kafka;
pub mod kinesis;
pub mod mysql;
pub mod odbc;
pub mod onelake;
pub mod open_table_catalog;
pub mod parquet;
pub mod postgres;
pub mod power_bi;
pub mod rest_api;
pub mod s3;
pub mod salesforce;
pub mod sap;
pub mod snowflake;
pub mod tableau;

#[derive(Debug, Clone, Serialize)]
pub struct ConnectionTestResult {
    pub success: bool,
    pub message: String,
    pub latency_ms: u128,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<Value>,
}

#[derive(Debug, Clone)]
pub struct SyncPayload {
    pub bytes: Vec<u8>,
    pub format: String,
    pub rows_synced: i64,
    pub file_name: String,
    pub metadata: Value,
}

pub fn add_source_signature(payload: &mut SyncPayload) {
    let signature = source_signature(&payload.bytes);
    if let Some(metadata) = payload.metadata.as_object_mut() {
        metadata.insert("source_signature".to_string(), Value::String(signature));
    }
}

pub fn source_signature(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);
    format!("sha256:{:x}", digest)
}

pub fn virtual_table_response(
    selector: &str,
    rows: Vec<Value>,
    metadata: Value,
) -> VirtualTableQueryResponse {
    let row_count = rows.len();
    let columns = rows
        .iter()
        .find_map(|row| {
            row.as_object()
                .map(|object| object.keys().cloned().collect())
        })
        .unwrap_or_default();
    let source_signature = serde_json::to_vec(&rows)
        .ok()
        .map(|bytes| source_signature(&bytes));
    VirtualTableQueryResponse {
        selector: selector.to_string(),
        mode: "zero_copy".to_string(),
        columns,
        row_count,
        rows,
        source_signature,
        metadata,
    }
}

pub fn basic_discovered_source(
    selector: impl Into<String>,
    display_name: impl Into<String>,
    source_kind: impl Into<String>,
    metadata: Value,
) -> DiscoveredSource {
    DiscoveredSource {
        selector: selector.into(),
        display_name: display_name.into(),
        source_kind: source_kind.into(),
        supports_sync: true,
        supports_zero_copy: true,
        source_signature: None,
        metadata,
    }
}

/// Materialise a column-keyed JSON row set as an Arrow IPC stream so the
/// dataset-versioning-service can ingest the result as a typed dataset version.
/// All columns are encoded as nullable Utf8 — sufficient for the productive
/// connectors that need a portable wire format without per-column type plumbing.
pub fn materialize_arrow_stream(columns: &[String], rows: &[Value]) -> Result<Vec<u8>, String> {
    let fields: Vec<Field> = columns
        .iter()
        .map(|name| Field::new(name, DataType::Utf8, true))
        .collect();
    let schema = Arc::new(Schema::new(fields));

    let mut column_data: Vec<Vec<Option<String>>> = columns
        .iter()
        .map(|_| Vec::with_capacity(rows.len()))
        .collect();
    for row in rows {
        for (idx, name) in columns.iter().enumerate() {
            let cell = match row.get(name) {
                Some(Value::Null) | None => None,
                Some(Value::String(s)) => Some(s.clone()),
                Some(other) => Some(other.to_string()),
            };
            column_data[idx].push(cell);
        }
    }
    let arrays: Vec<ArrayRef> = column_data
        .into_iter()
        .map(|values| Arc::new(StringArray::from(values)) as ArrayRef)
        .collect();
    let batch = RecordBatch::try_new(schema.clone(), arrays).map_err(|error| error.to_string())?;

    let mut buffer = Vec::with_capacity(4096);
    {
        let mut writer =
            StreamWriter::try_new(&mut buffer, &schema).map_err(|error| error.to_string())?;
        writer.write(&batch).map_err(|error| error.to_string())?;
        writer.finish().map_err(|error| error.to_string())?;
    }
    Ok(buffer)
}

/// Build a `SyncPayload` from in-memory rows, materialising them as an Arrow
/// IPC stream. The bytes are ready for `upload_dataset` to deliver to the
/// dataset-versioning-service so a new version is created.
pub fn arrow_payload_from_rows(
    file_name: impl Into<String>,
    columns: Vec<String>,
    rows: Vec<Value>,
    metadata: Value,
) -> Result<SyncPayload, String> {
    let bytes = materialize_arrow_stream(&columns, &rows)?;
    let mut payload = SyncPayload {
        bytes,
        format: "arrow".to_string(),
        rows_synced: rows.len() as i64,
        file_name: file_name.into(),
        metadata,
    };
    add_source_signature(&mut payload);
    Ok(payload)
}
