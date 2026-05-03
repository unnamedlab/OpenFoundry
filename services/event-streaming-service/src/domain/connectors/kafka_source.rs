use async_trait::async_trait;
use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::sync::Mutex;
use uuid::Uuid;

use super::source_trait::{
    ConnectorCheckpoint, ConnectorError, ConnectorHealth, ConnectorStatus, PullOptions,
    SourceRecord, StreamingSourceConnector,
};
use crate::models::{
    sink::{ConnectorCatalogEntry, LiveTailEvent},
    stream::StreamDefinition,
};

/// Operator-facing config persisted in
/// `streaming_streams.source_binding.config` for Kafka sources.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct KafkaSourceConfig {
    pub bootstrap_servers: String,
    pub topic: String,
    pub consumer_group: String,
    /// `earliest` or `latest`. Defaults to `latest` so a fresh sync
    /// only sees new records.
    #[serde(default = "default_offset_reset")]
    pub auto_offset_reset: String,
    /// `acks=all` is hard-coded; this knob just toggles the
    /// idempotent producer for sinks (unused on the source side).
    #[serde(default)]
    pub idempotent: bool,
}

fn default_offset_reset() -> String {
    "latest".to_string()
}

/// Pluggable Kafka pull surface. Production wires
/// [`super::super::hot_buffer::KafkaHotBuffer`]'s consumer; tests use
/// [`StaticKafkaClient`]. The trait keeps the pull path testable
/// without booting a broker.
#[async_trait]
pub trait KafkaSourceClient: Send + Sync + std::fmt::Debug {
    async fn poll(
        &self,
        topic: &str,
        max_records: u32,
        max_wait_ms: u64,
    ) -> Result<Vec<KafkaIncomingRecord>, ConnectorError>;

    /// Commit the offset map (`{partition: offset}`).
    async fn commit(&self, topic: &str, offsets: &Value) -> Result<(), ConnectorError>;
}

#[derive(Debug, Clone)]
pub struct KafkaIncomingRecord {
    pub partition: i32,
    pub offset: i64,
    pub key: Option<String>,
    pub payload: Vec<u8>,
    pub timestamp: DateTime<Utc>,
}

#[derive(Debug, Default)]
pub struct StaticKafkaClient {
    pub queued: Mutex<Vec<KafkaIncomingRecord>>,
    pub committed: Mutex<Vec<Value>>,
}

#[async_trait]
impl KafkaSourceClient for StaticKafkaClient {
    async fn poll(
        &self,
        _topic: &str,
        max_records: u32,
        _max_wait_ms: u64,
    ) -> Result<Vec<KafkaIncomingRecord>, ConnectorError> {
        let mut buf = self.queued.lock().expect("queued lock poisoned");
        let take = (max_records as usize).min(buf.len());
        Ok(buf.drain(..take).collect())
    }
    async fn commit(&self, _topic: &str, offsets: &Value) -> Result<(), ConnectorError> {
        if let Ok(mut log) = self.committed.lock() {
            log.push(offsets.clone());
        }
        Ok(())
    }
}

#[derive(Debug)]
pub struct KafkaConnector<C: KafkaSourceClient + 'static> {
    pub config: KafkaSourceConfig,
    pub client: C,
    pub last_pull: Mutex<Option<DateTime<Utc>>>,
}

impl<C: KafkaSourceClient + 'static> KafkaConnector<C> {
    pub fn new(config: KafkaSourceConfig, client: C) -> Self {
        Self {
            config,
            client,
            last_pull: Mutex::new(None),
        }
    }
}

#[async_trait]
impl<C: KafkaSourceClient + 'static> StreamingSourceConnector for KafkaConnector<C> {
    fn kind(&self) -> &'static str {
        "kafka"
    }

    async fn pull(&self, opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError> {
        let records = self
            .client
            .poll(&self.config.topic, opts.batch_size, opts.max_wait_ms)
            .await?;
        if let Ok(mut last) = self.last_pull.lock() {
            *last = Some(Utc::now());
        }
        if records.is_empty() {
            return Err(ConnectorError::Empty);
        }
        Ok(records
            .into_iter()
            .map(|r| {
                let payload = serde_json::from_slice::<Value>(&r.payload)
                    .unwrap_or_else(|_| Value::String(String::from_utf8_lossy(&r.payload).into()));
                SourceRecord {
                    source_id: format!("{}:{}", r.partition, r.offset),
                    partition_key: r.key.clone(),
                    payload,
                    event_time: r.timestamp,
                    metadata: serde_json::json!({
                        "partition": r.partition,
                        "offset": r.offset,
                        "topic": self.config.topic,
                    }),
                }
            })
            .collect())
    }

    async fn checkpoint(&self, checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError> {
        self.client
            .commit(&self.config.topic, &checkpoint.cursor)
            .await
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
        connector_type: "kafka".to_string(),
        direction: "source".to_string(),
        endpoint: stream.source_binding.endpoint.clone(),
        status: "healthy".to_string(),
        backlog: 18,
        throughput_per_second: 482.0,
        details: json!({
            "format": stream.source_binding.format,
            "consumer_group": stream.name.to_lowercase().replace(' ', "-")
        }),
    }
}

#[allow(dead_code)]
pub fn sample_events(stream: &StreamDefinition, topology_id: Uuid) -> Vec<LiveTailEvent> {
    let now = Utc::now();
    vec![
        LiveTailEvent {
            id: format!("{}-evt-1", stream.name.to_lowercase().replace(' ', "-")),
            topology_id,
            stream_name: stream.name.clone(),
            connector_type: "kafka".to_string(),
            payload: json!({
                "order_id": "ord-1842",
                "customer_id": "cust-22",
                "amount": 1420.5,
                "currency": "USD"
            }),
            event_time: now - Duration::seconds(14),
            processing_time: now - Duration::seconds(13),
            tags: vec![
                "join-key:customer_id".to_string(),
                "source:kafka".to_string(),
            ],
        },
        LiveTailEvent {
            id: format!("{}-evt-2", stream.name.to_lowercase().replace(' ', "-")),
            topology_id,
            stream_name: stream.name.clone(),
            connector_type: "kafka".to_string(),
            payload: json!({
                "order_id": "ord-1849",
                "customer_id": "cust-22",
                "amount": 310.0,
                "currency": "USD"
            }),
            event_time: now - Duration::seconds(7),
            processing_time: now - Duration::seconds(6),
            tags: vec!["window:5m".to_string(), "source:kafka".to_string()],
        },
    ]
}
