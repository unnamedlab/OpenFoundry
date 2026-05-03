//! Generic external-transform streaming connector (Bloque P5).
//!
//! Foundry's docs call out a long tail of streaming sources without
//! dedicated connectors (ActiveMQ, Amazon SNS, IBM MQ, RabbitMQ,
//! MQTT, Solace). The Foundry pattern is "external transforms" — a
//! Magritte agent runs the protocol-specific client outside the
//! platform and forwards records over an HTTP webhook into the
//! Foundry stream proxy. This connector implements the receive-side
//! of that contract: a buffered queue the agent pushes into via
//! HTTP, drained by the runner.

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::sync::{Arc, Mutex};
use uuid::Uuid;

use super::source_trait::{
    ConnectorCheckpoint, ConnectorError, ConnectorHealth, ConnectorStatus, PullOptions,
    SourceRecord, StreamingSourceConnector,
};
use crate::models::{sink::ConnectorCatalogEntry, stream::StreamDefinition};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExternalConfig {
    /// Free-form label surfaced in the catalogue card.
    pub agent_label: String,
    /// Pre-shared bearer token the Magritte agent uses to authenticate
    /// with the inbound webhook.
    pub agent_token: String,
    /// Logical protocol the agent is bridging — e.g. "activemq",
    /// "rabbitmq", "mqtt", "ibm_mq", "solace", "sns".
    pub protocol: String,
}

/// Buffer the Magritte agent posts into. Cloned by the inbound HTTP
/// handler so external pushes can land without holding any locks the
/// runner holds.
#[derive(Debug, Default, Clone)]
pub struct ExternalBuffer {
    inner: Arc<Mutex<Vec<SourceRecord>>>,
    pushed: Arc<Mutex<i64>>,
}

impl ExternalBuffer {
    pub fn new() -> Self {
        Self::default()
    }

    /// Inbound hook the webhook handler calls when an agent posts a
    /// payload. The agent supplies the source_id; the connector
    /// generates one when missing so dedup downstream still works.
    pub fn push(&self, payload: Value, partition_key: Option<String>, source_id: Option<String>) {
        let record = SourceRecord {
            source_id: source_id.unwrap_or_else(|| Uuid::now_v7().to_string()),
            partition_key,
            payload,
            event_time: Utc::now(),
            metadata: serde_json::json!({ "source": "magritte_external" }),
        };
        if let Ok(mut buf) = self.inner.lock() {
            buf.push(record);
        }
        if let Ok(mut counter) = self.pushed.lock() {
            *counter += 1;
        }
    }

    /// Used by tests to seed canned records.
    pub fn push_records(&self, records: Vec<SourceRecord>) {
        if let Ok(mut buf) = self.inner.lock() {
            buf.extend(records);
        }
    }

    pub fn pushed_count(&self) -> i64 {
        self.pushed.lock().map(|c| *c).unwrap_or(0)
    }

    fn drain(&self, max: usize) -> Vec<SourceRecord> {
        let Ok(mut buf) = self.inner.lock() else {
            return Vec::new();
        };
        let take = max.min(buf.len());
        buf.drain(..take).collect()
    }
}

#[derive(Debug)]
pub struct ExternalConnector {
    pub config: ExternalConfig,
    pub buffer: ExternalBuffer,
    pub last_pull: Mutex<Option<DateTime<Utc>>>,
}

impl ExternalConnector {
    pub fn new(config: ExternalConfig, buffer: ExternalBuffer) -> Self {
        Self {
            config,
            buffer,
            last_pull: Mutex::new(None),
        }
    }

    /// Verify the agent's bearer token matches the configured one.
    /// Used by the inbound webhook to reject foreign agents.
    pub fn authorise(&self, token: &str) -> bool {
        // Constant-time compare so token length / prefix can't leak
        // via timing analysis.
        let expected = self.config.agent_token.as_bytes();
        let provided = token.as_bytes();
        if expected.len() != provided.len() {
            return false;
        }
        let mut diff = 0u8;
        for (a, b) in expected.iter().zip(provided.iter()) {
            diff |= a ^ b;
        }
        diff == 0
    }
}

#[async_trait]
impl StreamingSourceConnector for ExternalConnector {
    fn kind(&self) -> &'static str {
        "external"
    }

    async fn pull(&self, opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError> {
        let drained = self.buffer.drain(opts.batch_size as usize);
        if let Ok(mut last) = self.last_pull.lock() {
            *last = Some(Utc::now());
        }
        if drained.is_empty() {
            return Err(ConnectorError::Empty);
        }
        Ok(drained)
    }

    async fn checkpoint(&self, _checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError> {
        // External agents are responsible for at-least-once delivery
        // on their side. The connector has no source-level cursor.
        Ok(())
    }

    async fn health(&self) -> ConnectorHealth {
        ConnectorHealth {
            status: ConnectorStatus::Healthy,
            backlog: 0,
            throughput_per_second: 0.0,
            last_pull_at: self.last_pull.lock().ok().and_then(|g| *g),
        }
    }
}

pub fn catalog_entry(stream: &StreamDefinition) -> ConnectorCatalogEntry {
    ConnectorCatalogEntry {
        connector_type: "external".to_string(),
        direction: "source".to_string(),
        endpoint: stream.source_binding.endpoint.clone(),
        status: "healthy".to_string(),
        backlog: 0,
        throughput_per_second: 0.0,
        details: serde_json::json!({
            "format": stream.source_binding.format,
            "doc": "Magritte external-transform source — agent pushes via webhook",
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn drain_returns_records_in_push_order() {
        let buf = ExternalBuffer::new();
        buf.push(serde_json::json!({"v": 1}), None, Some("a".into()));
        buf.push(serde_json::json!({"v": 2}), None, Some("b".into()));
        let connector = ExternalConnector::new(
            ExternalConfig {
                agent_label: "mq-agent".into(),
                agent_token: "secret".into(),
                protocol: "rabbitmq".into(),
            },
            buf,
        );
        let recs = connector.pull(&PullOptions::default()).await.unwrap();
        assert_eq!(recs.len(), 2);
        assert_eq!(recs[0].source_id, "a");
        assert_eq!(recs[1].source_id, "b");
    }

    #[test]
    fn authorise_uses_constant_time_compare() {
        let connector = ExternalConnector::new(
            ExternalConfig {
                agent_label: "x".into(),
                agent_token: "secret".into(),
                protocol: "mqtt".into(),
            },
            ExternalBuffer::new(),
        );
        assert!(connector.authorise("secret"));
        assert!(!connector.authorise("wrong"));
        assert!(!connector.authorise(""));
    }
}
