//! Parquet connector — reads Parquet files from URLs or local storage paths.
//!
//! This MVP implementation validates the Parquet magic markers (`PAR1` at
//! both ends of the file) and returns the raw bytes as the sync payload.
//! Row-level decoding (column projection, predicate pushdown, schema
//! introspection) is intentionally deferred: downstream services materialise
//! the bytes into the lake using `storage-abstraction`, which already depends
//! on the `parquet` crate. Keeping the connector free of that dependency
//! avoids pulling Arrow/Parquet into every connector build.
//!
//! See: docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//! Data formats/  (Parquet is treated as a file format on object storage)

use std::time::Instant;

use serde_json::{Value, json};
use tokio::fs;

use super::{ConnectionTestResult, SyncPayload, add_source_signature, virtual_table_response};
use crate::{
    AppState,
    models::registration::{VirtualTableQueryRequest, VirtualTableQueryResponse},
};

const PARQUET_MAGIC: &[u8; 4] = b"PAR1";

pub fn validate_config(config: &Value) -> Result<(), String> {
    if config.get("url").is_none() && config.get("path").is_none() {
        return Err("parquet connector requires either 'url' or 'path'".to_string());
    }
    Ok(())
}

pub async fn test_connection(
    state: &AppState,
    config: &Value,
) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;
    let started = Instant::now();
    let bytes = read_source(state, config).await?;
    validate_parquet_magic(&bytes)?;

    Ok(ConnectionTestResult {
        success: true,
        message: "Parquet source reachable".to_string(),
        latency_ms: started.elapsed().as_millis(),
        details: Some(json!({
            "bytes": bytes.len(),
            "source": source_label(config),
            "format": "parquet",
        })),
    })
}

pub async fn fetch_dataset(
    state: &AppState,
    config: &Value,
    selector: &str,
) -> Result<SyncPayload, String> {
    validate_config(config)?;
    let bytes = read_source(state, config).await?;
    validate_parquet_magic(&bytes)?;

    let mut payload = SyncPayload {
        bytes: bytes.clone(),
        format: "parquet".to_string(),
        // Parquet is columnar; row counting requires decoding the footer
        // metadata which is left to downstream materialisation. -1 signals
        // "unknown, decode at materialisation time".
        rows_synced: -1,
        file_name: file_name(config, selector, "parquet"),
        metadata: json!({
            "source": source_label(config),
            "bytes": bytes.len(),
            "format": "parquet",
        }),
    };
    add_source_signature(&mut payload);
    Ok(payload)
}

pub async fn query_virtual_table(
    state: &AppState,
    config: &Value,
    request: &VirtualTableQueryRequest,
) -> Result<VirtualTableQueryResponse, String> {
    validate_config(config)?;
    let bytes = read_source(state, config).await?;
    validate_parquet_magic(&bytes)?;

    // Without a Parquet decoder in this crate we cannot project rows; expose
    // the file as a single metadata row so the UI can surface "this is a
    // valid Parquet object, materialise it to inspect rows".
    let row = json!({
        "source": source_label(config),
        "bytes": bytes.len(),
        "format": "parquet",
        "note": "Row-level preview requires materialisation in the lake.",
    });
    Ok(virtual_table_response(
        &request.selector,
        vec![row],
        json!({
            "source": source_label(config),
            "format": "parquet",
        }),
    ))
}

async fn read_source(state: &AppState, config: &Value) -> Result<Vec<u8>, String> {
    if let Some(path) = config.get("path").and_then(Value::as_str) {
        return fs::read(path).await.map_err(|error| error.to_string());
    }

    let url = config
        .get("url")
        .and_then(Value::as_str)
        .ok_or_else(|| "parquet connector requires either 'url' or 'path'".to_string())?;
    let response = super::http_runtime::get(
        state,
        config,
        reqwest::Url::parse(url).map_err(|error| error.to_string())?,
        super::http_runtime::header_map(config)?,
        config
            .get("bearer_token")
            .and_then(Value::as_str)
            .map(|value| value.to_string()),
        None,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!("Parquet source returned HTTP {}", response.status));
    }
    Ok(response.bytes)
}

fn validate_parquet_magic(bytes: &[u8]) -> Result<(), String> {
    if bytes.len() < 8 {
        return Err(format!(
            "parquet file too small ({} bytes) — missing magic markers",
            bytes.len()
        ));
    }
    if &bytes[..4] != PARQUET_MAGIC {
        return Err("parquet header magic 'PAR1' missing at start of file".to_string());
    }
    if &bytes[bytes.len() - 4..] != PARQUET_MAGIC {
        return Err("parquet footer magic 'PAR1' missing at end of file".to_string());
    }
    Ok(())
}

fn file_name(config: &Value, selector: &str, fallback_ext: &str) -> String {
    config
        .get("path")
        .and_then(Value::as_str)
        .and_then(|path| std::path::Path::new(path).file_name())
        .and_then(|value| value.to_str())
        .map(str::to_string)
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| {
            let stem: String = selector
                .chars()
                .map(|ch| if ch.is_ascii_alphanumeric() { ch } else { '_' })
                .collect();
            let trimmed = stem.trim_matches('_');
            let stem = if trimmed.is_empty() {
                "parquet_sync"
            } else {
                trimmed
            };
            format!("{stem}.{fallback_ext}")
        })
}

fn source_label(config: &Value) -> String {
    config
        .get("path")
        .and_then(Value::as_str)
        .map(str::to_string)
        .or_else(|| {
            config
                .get("url")
                .and_then(Value::as_str)
                .map(str::to_string)
        })
        .unwrap_or_else(|| "parquet".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_config_requires_url_or_path() {
        assert!(validate_config(&json!({})).is_err());
        assert!(validate_config(&json!({ "url": "https://example.com/a.parquet" })).is_ok());
        assert!(validate_config(&json!({ "path": "/tmp/a.parquet" })).is_ok());
    }

    #[test]
    fn magic_validation_accepts_minimal_envelope() {
        let mut bytes = b"PAR1".to_vec();
        bytes.extend_from_slice(&[0u8; 8]); // padding for "metadata"
        bytes.extend_from_slice(b"PAR1");
        assert!(validate_parquet_magic(&bytes).is_ok());
    }

    #[test]
    fn magic_validation_rejects_truncated_files() {
        assert!(validate_parquet_magic(b"PAR1").is_err());
    }

    #[test]
    fn magic_validation_rejects_wrong_header() {
        let bytes = [b'X', b'X', b'X', b'X', 0, 0, 0, 0, b'P', b'A', b'R', b'1'];
        assert!(validate_parquet_magic(&bytes).is_err());
    }

    #[test]
    fn magic_validation_rejects_wrong_footer() {
        let bytes = [b'P', b'A', b'R', b'1', 0, 0, 0, 0, b'X', b'X', b'X', b'X'];
        assert!(validate_parquet_magic(&bytes).is_err());
    }
}
