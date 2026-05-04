//! Google Cloud Pub/Sub streaming-source connector (Bloque P5).
//!
//! Uses the REST `pull` API (https://pubsub.googleapis.com) so we
//! avoid pulling in `google-cloud-pubsub` (gRPC + tonic). The runner
//! pulls a batch, ack-deadline-extends if the downstream commit takes
//! longer than the subscription's default, and acks once the records
//! have landed in the hot buffer.

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
pub struct PubSubConfig {
    pub project_id: String,
    pub subscription_id: String,
    /// Max ack ids the runner asks for per pull. Pub/Sub honours the
    /// number as a soft cap (it may return fewer).
    #[serde(default = "default_max")]
    pub max_messages: u32,
    /// Per-pull ack deadline override in seconds (10..=600). Defaults
    /// to 60s, which is enough for a normal hot-buffer publish.
    #[serde(default = "default_ack_deadline_seconds")]
    pub ack_deadline_seconds: u32,
}

fn default_max() -> u32 {
    100
}
fn default_ack_deadline_seconds() -> u32 {
    60
}

#[derive(Debug, Clone)]
pub struct PubSubMessage {
    pub message_id: String,
    pub ack_id: String,
    pub data: Vec<u8>,
    pub publish_time: DateTime<Utc>,
    pub attributes: serde_json::Map<String, Value>,
}

#[async_trait]
pub trait PubSubClient: Send + Sync + std::fmt::Debug {
    async fn pull(
        &self,
        subscription: &str,
        max_messages: u32,
    ) -> Result<Vec<PubSubMessage>, ConnectorError>;
    async fn acknowledge(
        &self,
        subscription: &str,
        ack_ids: &[String],
    ) -> Result<(), ConnectorError>;
    async fn modify_ack_deadline(
        &self,
        subscription: &str,
        ack_ids: &[String],
        deadline_seconds: u32,
    ) -> Result<(), ConnectorError>;
}

#[derive(Debug, Default)]
pub struct StaticPubSubClient {
    pub queued: Mutex<Vec<PubSubMessage>>,
    pub acked: Mutex<Vec<String>>,
    pub deadline_extensions: Mutex<Vec<(String, u32)>>,
}

#[async_trait]
impl PubSubClient for StaticPubSubClient {
    async fn pull(
        &self,
        _subscription: &str,
        max_messages: u32,
    ) -> Result<Vec<PubSubMessage>, ConnectorError> {
        let mut buf = self.queued.lock().expect("queued lock poisoned");
        let take = (max_messages as usize).min(buf.len());
        Ok(buf.drain(..take).collect())
    }
    async fn acknowledge(
        &self,
        _subscription: &str,
        ack_ids: &[String],
    ) -> Result<(), ConnectorError> {
        if let Ok(mut log) = self.acked.lock() {
            log.extend(ack_ids.iter().cloned());
        }
        Ok(())
    }
    async fn modify_ack_deadline(
        &self,
        _subscription: &str,
        ack_ids: &[String],
        deadline_seconds: u32,
    ) -> Result<(), ConnectorError> {
        if let Ok(mut log) = self.deadline_extensions.lock() {
            for id in ack_ids {
                log.push((id.clone(), deadline_seconds));
            }
        }
        Ok(())
    }
}

/// Production HTTP client. SigV4-equivalent auth lives in the
/// `Authorization: Bearer <oauth2-token>` header. The token is
/// resolved by the caller (workload identity / metadata server).
#[derive(Debug, Clone)]
pub struct HttpPubSubClient {
    pub base_url: String,
    pub bearer_token: Option<String>,
    pub http: reqwest::Client,
}

impl HttpPubSubClient {
    fn full_subscription(&self, project_id: &str, subscription_id: &str) -> String {
        format!("projects/{project_id}/subscriptions/{subscription_id}")
    }
}

#[async_trait]
impl PubSubClient for HttpPubSubClient {
    async fn pull(
        &self,
        subscription: &str,
        max_messages: u32,
    ) -> Result<Vec<PubSubMessage>, ConnectorError> {
        let body = serde_json::json!({
            "maxMessages": max_messages,
            "returnImmediately": false
        });
        let mut req = self
            .http
            .post(format!(
                "{}/v1/{subscription}:pull",
                self.base_url.trim_end_matches('/')
            ))
            .json(&body);
        if let Some(token) = self.bearer_token.as_deref() {
            req = req.bearer_auth(token);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| ConnectorError::Transport(e.to_string()))?;
        if !resp.status().is_success() {
            return Err(ConnectorError::Transport(format!(
                "pubsub pull status {}",
                resp.status()
            )));
        }
        #[derive(Deserialize)]
        struct Raw {
            #[serde(default, rename = "receivedMessages")]
            received: Vec<RawReceived>,
        }
        #[derive(Deserialize)]
        struct RawReceived {
            #[serde(default, rename = "ackId")]
            ack_id: String,
            message: RawMessage,
        }
        #[derive(Deserialize)]
        struct RawMessage {
            #[serde(default, rename = "messageId")]
            message_id: String,
            #[serde(default)]
            data: String,
            #[serde(default, rename = "publishTime")]
            publish_time: String,
            #[serde(default)]
            attributes: Option<serde_json::Map<String, Value>>,
        }
        let raw: Raw = resp
            .json()
            .await
            .map_err(|e| ConnectorError::Decode(e.to_string()))?;
        Ok(raw
            .received
            .into_iter()
            .map(|r| {
                use base64::Engine;
                let data = base64::engine::general_purpose::STANDARD
                    .decode(r.message.data.as_bytes())
                    .unwrap_or_default();
                let publish_time = DateTime::parse_from_rfc3339(&r.message.publish_time)
                    .map(|t| t.with_timezone(&Utc))
                    .unwrap_or_else(|_| Utc::now());
                PubSubMessage {
                    message_id: r.message.message_id,
                    ack_id: r.ack_id,
                    data,
                    publish_time,
                    attributes: r.message.attributes.unwrap_or_default(),
                }
            })
            .collect())
    }

    async fn acknowledge(
        &self,
        subscription: &str,
        ack_ids: &[String],
    ) -> Result<(), ConnectorError> {
        let body = serde_json::json!({ "ackIds": ack_ids });
        let mut req = self
            .http
            .post(format!(
                "{}/v1/{subscription}:acknowledge",
                self.base_url.trim_end_matches('/')
            ))
            .json(&body);
        if let Some(token) = self.bearer_token.as_deref() {
            req = req.bearer_auth(token);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| ConnectorError::Transport(e.to_string()))?;
        if !resp.status().is_success() {
            return Err(ConnectorError::Transport(format!(
                "pubsub ack status {}",
                resp.status()
            )));
        }
        Ok(())
    }

    async fn modify_ack_deadline(
        &self,
        subscription: &str,
        ack_ids: &[String],
        deadline_seconds: u32,
    ) -> Result<(), ConnectorError> {
        let body = serde_json::json!({
            "ackIds": ack_ids,
            "ackDeadlineSeconds": deadline_seconds
        });
        let mut req = self
            .http
            .post(format!(
                "{}/v1/{subscription}:modifyAckDeadline",
                self.base_url.trim_end_matches('/')
            ))
            .json(&body);
        if let Some(token) = self.bearer_token.as_deref() {
            req = req.bearer_auth(token);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| ConnectorError::Transport(e.to_string()))?;
        if !resp.status().is_success() {
            return Err(ConnectorError::Transport(format!(
                "pubsub modifyAckDeadline status {}",
                resp.status()
            )));
        }
        let _ = self.full_subscription("", ""); // silence unused-helper lint
        Ok(())
    }
}

#[derive(Debug)]
pub struct PubSubConnector<C: PubSubClient + 'static> {
    pub config: PubSubConfig,
    pub client: C,
    pub last_pull: Mutex<Option<DateTime<Utc>>>,
}

impl<C: PubSubClient + 'static> PubSubConnector<C> {
    pub fn new(config: PubSubConfig, client: C) -> Self {
        Self {
            config,
            client,
            last_pull: Mutex::new(None),
        }
    }
    fn subscription(&self) -> String {
        format!(
            "projects/{}/subscriptions/{}",
            self.config.project_id, self.config.subscription_id
        )
    }
}

#[async_trait]
impl<C: PubSubClient + 'static> StreamingSourceConnector for PubSubConnector<C> {
    fn kind(&self) -> &'static str {
        "pubsub"
    }

    async fn pull(&self, opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError> {
        let n = opts.batch_size.min(self.config.max_messages);
        let msgs = self.client.pull(&self.subscription(), n).await?;
        if let Ok(mut last) = self.last_pull.lock() {
            *last = Some(Utc::now());
        }
        if msgs.is_empty() {
            return Err(ConnectorError::Empty);
        }
        // Extend ack deadline so a slow downstream commit doesn't
        // cause Pub/Sub to redeliver the batch underneath us.
        let ack_ids: Vec<String> = msgs.iter().map(|m| m.ack_id.clone()).collect();
        let _ = self
            .client
            .modify_ack_deadline(
                &self.subscription(),
                &ack_ids,
                self.config.ack_deadline_seconds,
            )
            .await;
        Ok(msgs
            .into_iter()
            .map(|m| {
                let payload = serde_json::from_slice::<Value>(&m.data).unwrap_or_else(|_| {
                    Value::String(String::from_utf8_lossy(&m.data).to_string())
                });
                SourceRecord {
                    source_id: m.message_id.clone(),
                    partition_key: m
                        .attributes
                        .get("ordering_key")
                        .and_then(|v| v.as_str())
                        .map(str::to_string),
                    payload,
                    event_time: m.publish_time,
                    metadata: serde_json::json!({
                        "ack_id": m.ack_id,
                        "attributes": m.attributes,
                    }),
                }
            })
            .collect())
    }

    async fn checkpoint(&self, _checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError> {
        // Pub/Sub progresses via per-message ack — no separate
        // checkpoint cursor.
        Ok(())
    }

    async fn ack(&self, record: &SourceRecord) -> Result<(), ConnectorError> {
        let ack_id = record
            .metadata
            .get("ack_id")
            .and_then(|v| v.as_str())
            .ok_or_else(|| ConnectorError::Decode("missing ack_id".into()))?;
        self.client
            .acknowledge(&self.subscription(), &[ack_id.to_string()])
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
        connector_type: "pubsub".to_string(),
        direction: "source".to_string(),
        endpoint: stream.source_binding.endpoint.clone(),
        status: "healthy".to_string(),
        backlog: 0,
        throughput_per_second: 0.0,
        details: serde_json::json!({
            "format": stream.source_binding.format,
            "doc": "Google Cloud Pub/Sub source — REST pull + ack",
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn extends_ack_deadline_on_each_pull() {
        let client = StaticPubSubClient::default();
        client.queued.lock().unwrap().push(PubSubMessage {
            message_id: "m-1".into(),
            ack_id: "ack-1".into(),
            data: b"{\"v\":1}".to_vec(),
            publish_time: Utc::now(),
            attributes: Default::default(),
        });
        let connector = PubSubConnector::new(
            PubSubConfig {
                project_id: "p".into(),
                subscription_id: "s".into(),
                max_messages: 10,
                ack_deadline_seconds: 90,
            },
            client,
        );
        let recs = connector.pull(&PullOptions::default()).await.unwrap();
        assert_eq!(recs.len(), 1);
        let exts = connector.client.deadline_extensions.lock().unwrap().clone();
        assert_eq!(exts, vec![("ack-1".to_string(), 90)]);
    }

    #[tokio::test]
    async fn ack_acknowledges_one_message() {
        let client = StaticPubSubClient::default();
        client.queued.lock().unwrap().push(PubSubMessage {
            message_id: "m-1".into(),
            ack_id: "ack-1".into(),
            data: b"hi".to_vec(),
            publish_time: Utc::now(),
            attributes: Default::default(),
        });
        let connector = PubSubConnector::new(
            PubSubConfig {
                project_id: "p".into(),
                subscription_id: "s".into(),
                max_messages: 10,
                ack_deadline_seconds: 60,
            },
            client,
        );
        let rec = connector
            .pull(&PullOptions::default())
            .await
            .unwrap()
            .remove(0);
        connector.ack(&rec).await.unwrap();
        let acked = connector.client.acked.lock().unwrap().clone();
        assert_eq!(acked, vec!["ack-1".to_string()]);
    }
}
