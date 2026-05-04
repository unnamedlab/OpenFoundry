//! Amazon SQS streaming-source connector (Bloque P5).
//!
//! Long-poll-based pull with explicit per-message ack
//! (`DeleteMessage`). Records that are not deleted before
//! `visibility_timeout` are redelivered by SQS automatically; the
//! runner gets at-least-once semantics.

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::sync::Mutex;

use super::source_trait::{
    ConnectorCheckpoint, ConnectorError, ConnectorHealth, ConnectorStatus, PullOptions,
    SourceRecord, StreamingSourceConnector,
};
use crate::models::{sink::ConnectorCatalogEntry, stream::StreamDefinition};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SqsConfig {
    pub queue_url: String,
    pub region: String,
    /// `WaitTimeSeconds` for long polling (0..=20). 20s is the
    /// Foundry-default for low-volume queues.
    #[serde(default = "default_wait_seconds")]
    pub wait_time_seconds: u32,
    /// Visibility timeout applied to delivered messages, in seconds.
    /// Must exceed the worst-case time the runner needs to push a
    /// batch into the hot buffer.
    #[serde(default = "default_visibility_seconds")]
    pub visibility_timeout_seconds: u32,
}

fn default_wait_seconds() -> u32 {
    20
}
fn default_visibility_seconds() -> u32 {
    60
}

#[derive(Debug, Clone)]
pub struct SqsMessage {
    pub message_id: String,
    pub receipt_handle: String,
    pub body: String,
    pub sent_at: DateTime<Utc>,
}

#[async_trait]
pub trait SqsClient: Send + Sync + std::fmt::Debug {
    async fn receive_messages(
        &self,
        queue_url: &str,
        max_messages: u32,
        wait_seconds: u32,
        visibility_seconds: u32,
    ) -> Result<Vec<SqsMessage>, ConnectorError>;

    async fn delete_message(
        &self,
        queue_url: &str,
        receipt_handle: &str,
    ) -> Result<(), ConnectorError>;
}

#[derive(Debug, Default)]
pub struct StaticSqsClient {
    pub queued: Mutex<Vec<SqsMessage>>,
    pub deleted: Mutex<Vec<String>>,
}

#[async_trait]
impl SqsClient for StaticSqsClient {
    async fn receive_messages(
        &self,
        _queue_url: &str,
        max_messages: u32,
        _wait_seconds: u32,
        _visibility_seconds: u32,
    ) -> Result<Vec<SqsMessage>, ConnectorError> {
        let mut buf = self.queued.lock().expect("queued lock poisoned");
        let take = (max_messages as usize).min(buf.len());
        Ok(buf.drain(..take).collect())
    }
    async fn delete_message(
        &self,
        _queue_url: &str,
        receipt_handle: &str,
    ) -> Result<(), ConnectorError> {
        if let Ok(mut log) = self.deleted.lock() {
            log.push(receipt_handle.to_string());
        }
        Ok(())
    }
}

#[derive(Debug)]
pub struct SqsConnector<C: SqsClient + 'static> {
    pub config: SqsConfig,
    pub client: C,
    pub last_pull: Mutex<Option<DateTime<Utc>>>,
}

impl<C: SqsClient + 'static> SqsConnector<C> {
    pub fn new(config: SqsConfig, client: C) -> Self {
        Self {
            config,
            client,
            last_pull: Mutex::new(None),
        }
    }
}

#[async_trait]
impl<C: SqsClient + 'static> StreamingSourceConnector for SqsConnector<C> {
    fn kind(&self) -> &'static str {
        "sqs"
    }

    async fn pull(&self, opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError> {
        let max = opts.batch_size.min(10); // SQS API hard cap is 10.
        let messages = self
            .client
            .receive_messages(
                &self.config.queue_url,
                max,
                self.config.wait_time_seconds,
                self.config.visibility_timeout_seconds,
            )
            .await?;
        if let Ok(mut last) = self.last_pull.lock() {
            *last = Some(Utc::now());
        }
        if messages.is_empty() {
            return Err(ConnectorError::Empty);
        }
        Ok(messages
            .into_iter()
            .map(|m| {
                let payload = serde_json::from_str::<Value>(&m.body)
                    .unwrap_or_else(|_| Value::String(m.body.clone()));
                SourceRecord {
                    source_id: m.message_id.clone(),
                    partition_key: None,
                    payload,
                    event_time: m.sent_at,
                    metadata: serde_json::json!({
                        "receipt_handle": m.receipt_handle,
                        "queue_url": self.config.queue_url,
                    }),
                }
            })
            .collect())
    }

    async fn checkpoint(&self, _checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError> {
        // SQS commits per-message via DeleteMessage in `ack`; the
        // runner can persist the last receipt handle for diagnostics
        // but the source itself doesn't need it.
        Ok(())
    }

    async fn ack(&self, record: &SourceRecord) -> Result<(), ConnectorError> {
        let handle = record
            .metadata
            .get("receipt_handle")
            .and_then(|v| v.as_str())
            .ok_or_else(|| ConnectorError::Decode("missing receipt_handle".into()))?;
        self.client
            .delete_message(&self.config.queue_url, handle)
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
        connector_type: "sqs".to_string(),
        direction: "source".to_string(),
        endpoint: stream.source_binding.endpoint.clone(),
        status: "healthy".to_string(),
        backlog: 0,
        throughput_per_second: 0.0,
        details: serde_json::json!({
            "format": stream.source_binding.format,
            "doc": "Amazon SQS source — long-poll + per-message ack",
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn ack_calls_delete_message() {
        let client = StaticSqsClient::default();
        client.queued.lock().unwrap().push(SqsMessage {
            message_id: "m-1".into(),
            receipt_handle: "rcpt-1".into(),
            body: "{\"v\":1}".into(),
            sent_at: Utc::now(),
        });
        let connector = SqsConnector::new(
            SqsConfig {
                queue_url: "https://sqs.example.com/q".into(),
                region: "us-east-1".into(),
                wait_time_seconds: 20,
                visibility_timeout_seconds: 60,
            },
            client,
        );
        let recs = connector.pull(&PullOptions::default()).await.unwrap();
        assert_eq!(recs.len(), 1);
        connector.ack(&recs[0]).await.unwrap();
        let deleted = connector.client.deleted.lock().unwrap();
        assert_eq!(deleted.as_slice(), &["rcpt-1".to_string()]);
    }
}
