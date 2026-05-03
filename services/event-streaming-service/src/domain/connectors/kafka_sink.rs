//! Kafka sink (Bloque E4).
//!
//! Pushes already-serialised stream events to a Kafka topic via the
//! shared [`crate::backends::BackendRegistry`]. The connector binding
//! must look like `kafka://<topic>` and (optionally) carry
//! `bootstrap.servers` in its `config` payload — the actual broker
//! configuration is set service-wide at startup, this module only
//! issues `Backend::publish` calls.
//!
//! When the Kafka backend isn't registered (e.g. the service is running
//! without rdkafka enabled) we surface a clean error instead of
//! attempting to fall back to NATS — sinks must be deterministic.

use std::collections::BTreeMap;
use std::sync::Arc;

use bytes::Bytes;
use serde_json::json;

use crate::backends::{Backend, BackendError, Envelope};
use crate::models::{sink::ConnectorCatalogEntry, stream::ConnectorBinding};
use crate::router::BackendId;

pub fn catalog_entry(binding: &ConnectorBinding) -> ConnectorCatalogEntry {
    ConnectorCatalogEntry {
        connector_type: "kafka".to_string(),
        direction: "sink".to_string(),
        endpoint: binding.endpoint.clone(),
        status: "ready".to_string(),
        backlog: 0,
        throughput_per_second: 240.0,
        details: json!({
            "format": binding.format,
            "delivery": "at-least-once",
        }),
    }
}

#[derive(Debug, thiserror::Error)]
pub enum KafkaSinkError {
    #[error("invalid binding: {0}")]
    InvalidBinding(String),
    #[error("kafka backend not registered")]
    BackendMissing,
    #[error("kafka publish failed: {0}")]
    Publish(#[from] BackendError),
    #[error("encoding error: {0}")]
    Encoding(String),
}

#[derive(Debug, Clone)]
pub struct KafkaSinkOutcome {
    pub topic: String,
    pub published_events: usize,
}

/// Publish `events` to the Kafka topic encoded in `binding.endpoint`.
/// Each event is serialised as JSON and tagged with the stream metadata
/// in headers so downstream consumers can filter without parsing the
/// payload.
pub async fn publish_events(
    backend: Option<Arc<dyn Backend>>,
    binding: &ConnectorBinding,
    headers_extra: &BTreeMap<String, String>,
    events: &[serde_json::Value],
) -> Result<KafkaSinkOutcome, KafkaSinkError> {
    let topic = parse_topic(&binding.endpoint)?;
    let backend = backend.ok_or(KafkaSinkError::BackendMissing)?;
    if backend.id() != BackendId::Kafka {
        return Err(KafkaSinkError::BackendMissing);
    }
    for event in events {
        let payload =
            serde_json::to_vec(event).map_err(|e| KafkaSinkError::Encoding(e.to_string()))?;
        let mut headers = headers_extra.clone();
        headers.insert("content-type".to_string(), "application/json".to_string());
        let envelope = Envelope {
            topic: topic.clone(),
            payload: Bytes::from(payload),
            headers,
            schema_id: None,
        };
        backend.publish(envelope).await?;
    }
    Ok(KafkaSinkOutcome {
        topic,
        published_events: events.len(),
    })
}

fn parse_topic(endpoint: &str) -> Result<String, KafkaSinkError> {
    let stripped = endpoint.strip_prefix("kafka://").ok_or_else(|| {
        KafkaSinkError::InvalidBinding(format!("expected kafka://… got '{endpoint}'"))
    })?;
    if stripped.is_empty() {
        return Err(KafkaSinkError::InvalidBinding(
            "kafka endpoint must include a topic".to_string(),
        ));
    }
    Ok(stripped.trim_matches('/').to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_topic_strips_kafka_scheme() {
        assert_eq!(parse_topic("kafka://orders").unwrap(), "orders");
    }

    #[test]
    fn parse_topic_rejects_non_kafka_scheme() {
        assert!(parse_topic("dataset://orders").is_err());
        assert!(parse_topic("kafka://").is_err());
    }
}
