//! Real dataset sink (Bloque E4).
//!
//! Until E4 this module only returned a *catalog entry* (mock metadata).
//! Now it can also commit a batch of stream events to the Iceberg-backed
//! [`DatasetWriter`] held by [`crate::AppState::dataset_writer`] and
//! notify `data-asset-catalog-service` via plain HTTP.

use std::sync::Arc;

use bytes::Bytes;
use serde_json::json;

use crate::models::{
    sink::ConnectorCatalogEntry,
    stream::{ConnectorBinding, StreamDefinition},
};
use crate::storage::{DatasetSnapshot, DatasetWriter, WriteOutcome, WriterError};

pub fn catalog_entry(binding: &ConnectorBinding) -> ConnectorCatalogEntry {
    ConnectorCatalogEntry {
        connector_type: "dataset".to_string(),
        direction: "sink".to_string(),
        endpoint: binding.endpoint.clone(),
        status: "ready".to_string(),
        backlog: 3,
        throughput_per_second: 180.0,
        details: json!({
            "format": binding.format,
            "mode": "iceberg-incremental",
        }),
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SinkError {
    #[error("invalid binding: {0}")]
    InvalidBinding(String),
    #[error("encoding error: {0}")]
    Encoding(String),
    #[error("writer error: {0}")]
    Writer(#[from] WriterError),
}

#[derive(Debug, Clone)]
pub struct DatasetSinkOutcome {
    pub write: WriteOutcome,
    pub catalog_notified: bool,
    pub warnings: Vec<String>,
}

/// Commit `events` to the dataset referenced by `binding.endpoint`. The
/// endpoint is expected to look like `dataset://<table_name>`.
pub async fn commit_events(
    writer: Arc<dyn DatasetWriter>,
    http: &reqwest::Client,
    catalog_base_url: &str,
    stream: &StreamDefinition,
    binding: &ConnectorBinding,
    events: &[serde_json::Value],
) -> Result<DatasetSinkOutcome, SinkError> {
    let table = parse_table(&binding.endpoint)?;

    let payload_text = events
        .iter()
        .map(serde_json::to_string)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| SinkError::Encoding(e.to_string()))?
        .join("\n");
    let payload = Bytes::from(payload_text.into_bytes());
    let snapshot_id = format!("snap-{}", uuid::Uuid::now_v7().simple());

    let snapshot =
        DatasetSnapshot::new(table.clone(), snapshot_id.clone(), payload).with_metadata(json!({
            "stream_id": stream.id,
            "stream_name": stream.name,
            "events": events.len(),
            "consistency_guarantee": stream.consistency_guarantee,
            "default_marking": stream.default_marking,
        }));

    let outcome = writer.append(snapshot).await?;

    let mut warnings = Vec::new();
    let catalog_notified = match notify_catalog(
        http,
        catalog_base_url,
        &table,
        &snapshot_id,
        &outcome.location,
        events.len(),
        stream,
    )
    .await
    {
        Ok(ok) => ok,
        Err(err) => {
            warnings.push(format!("catalog notification failed: {err}"));
            false
        }
    };

    Ok(DatasetSinkOutcome {
        write: outcome,
        catalog_notified,
        warnings,
    })
}

fn parse_table(endpoint: &str) -> Result<String, SinkError> {
    let stripped = endpoint.strip_prefix("dataset://").ok_or_else(|| {
        SinkError::InvalidBinding(format!("expected dataset://… got '{endpoint}'"))
    })?;
    if stripped.is_empty() {
        return Err(SinkError::InvalidBinding(
            "dataset endpoint must include a table name".to_string(),
        ));
    }
    Ok(stripped.trim_matches('/').to_string())
}

async fn notify_catalog(
    http: &reqwest::Client,
    base_url: &str,
    table: &str,
    snapshot_id: &str,
    location: &str,
    event_count: usize,
    stream: &StreamDefinition,
) -> Result<bool, String> {
    if base_url.is_empty() {
        return Ok(false);
    }
    let url = format!(
        "{}/api/v1/datasets/{}:append-snapshot",
        base_url.trim_end_matches('/'),
        table
    );
    let body = json!({
        "snapshot_id": snapshot_id,
        "location": location,
        "event_count": event_count,
        "stream_id": stream.id,
        "stream_name": stream.name,
        "marking": stream.default_marking,
    });
    let resp = http
        .post(&url)
        .json(&body)
        .send()
        .await
        .map_err(|e| e.to_string())?;
    if resp.status().is_success() {
        Ok(true)
    } else {
        Err(format!("catalog returned {}", resp.status()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_table_strips_dataset_scheme() {
        assert_eq!(parse_table("dataset://orders").unwrap(), "orders");
        assert_eq!(parse_table("dataset:///orders/v2").unwrap(), "orders/v2");
    }

    #[test]
    fn parse_table_rejects_other_schemes() {
        assert!(parse_table("kafka://orders").is_err());
        assert!(parse_table("dataset://").is_err());
    }
}
