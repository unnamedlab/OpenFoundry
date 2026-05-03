//! Aveva PI Web API streaming source connector (Bloque P5).
//!
//! Pulls observations from a PI Web API endpoint over HTTPS by
//! polling either an event stream id or a list of attribute web ids.
//! The connector tracks the last `Timestamp` it observed so a restart
//! resumes at the right point — PI Web API's event-stream
//! subscriptions also expose a per-marker continuation token.

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
pub struct AvevaPiConfig {
    /// Base URL of the PI Web API, e.g.
    /// `https://piweb.example.com/piwebapi`.
    pub base_url: String,
    /// Either an event-stream WebID (preferred) or an attribute path.
    pub event_stream_web_id: String,
    /// Polling cadence (ms). PI doesn't push; the runner polls.
    #[serde(default = "default_poll_ms")]
    pub poll_interval_ms: u64,
    /// Optional Authorization header (Basic, Bearer, …) — the PI Web
    /// API supports both.
    #[serde(default)]
    pub auth_header: Option<String>,
}

fn default_poll_ms() -> u64 {
    2_000
}

/// Single PI observation. The PI Web API returns these as
/// `{Timestamp, Value, Good, Questionable, Substituted}` objects.
#[derive(Debug, Clone)]
pub struct PiObservation {
    pub timestamp: DateTime<Utc>,
    pub value: Value,
    pub good: bool,
    pub questionable: bool,
    pub substituted: bool,
}

#[async_trait]
pub trait AvevaPiClient: Send + Sync + std::fmt::Debug {
    async fn poll(
        &self,
        web_id: &str,
        since: Option<DateTime<Utc>>,
    ) -> Result<Vec<PiObservation>, ConnectorError>;
}

#[derive(Debug, Default)]
pub struct StaticAvevaPiClient {
    pub queued: Mutex<Vec<PiObservation>>,
}

#[async_trait]
impl AvevaPiClient for StaticAvevaPiClient {
    async fn poll(
        &self,
        _web_id: &str,
        _since: Option<DateTime<Utc>>,
    ) -> Result<Vec<PiObservation>, ConnectorError> {
        let mut buf = self.queued.lock().expect("queued lock poisoned");
        Ok(buf.drain(..).collect())
    }
}

#[derive(Debug, Clone)]
pub struct HttpAvevaPiClient {
    pub base_url: String,
    pub auth_header: Option<String>,
    pub http: reqwest::Client,
}

#[async_trait]
impl AvevaPiClient for HttpAvevaPiClient {
    async fn poll(
        &self,
        web_id: &str,
        since: Option<DateTime<Utc>>,
    ) -> Result<Vec<PiObservation>, ConnectorError> {
        let mut url = format!(
            "{}/streams/{web_id}/recorded",
            self.base_url.trim_end_matches('/')
        );
        if let Some(ts) = since {
            url.push_str(&format!("?startTime={}", ts.to_rfc3339()));
        }
        let mut req = self.http.get(url);
        if let Some(auth) = self.auth_header.as_deref() {
            req = req.header("Authorization", auth);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| ConnectorError::Transport(e.to_string()))?;
        if !resp.status().is_success() {
            return Err(ConnectorError::Transport(format!(
                "aveva pi status {}",
                resp.status()
            )));
        }
        #[derive(Deserialize)]
        struct Raw {
            #[serde(default, rename = "Items")]
            items: Vec<RawItem>,
        }
        #[derive(Deserialize)]
        struct RawItem {
            #[serde(rename = "Timestamp")]
            timestamp: String,
            #[serde(default, rename = "Value")]
            value: Value,
            #[serde(default, rename = "Good")]
            good: bool,
            #[serde(default, rename = "Questionable")]
            questionable: bool,
            #[serde(default, rename = "Substituted")]
            substituted: bool,
        }
        let raw: Raw = resp
            .json()
            .await
            .map_err(|e| ConnectorError::Decode(e.to_string()))?;
        Ok(raw
            .items
            .into_iter()
            .map(|it| PiObservation {
                timestamp: DateTime::parse_from_rfc3339(&it.timestamp)
                    .map(|t| t.with_timezone(&Utc))
                    .unwrap_or_else(|_| Utc::now()),
                value: it.value,
                good: it.good,
                questionable: it.questionable,
                substituted: it.substituted,
            })
            .collect())
    }
}

#[derive(Debug)]
pub struct AvevaPiConnector<C: AvevaPiClient + 'static> {
    pub config: AvevaPiConfig,
    pub client: C,
    pub last_seen: Mutex<Option<DateTime<Utc>>>,
}

impl<C: AvevaPiClient + 'static> AvevaPiConnector<C> {
    pub fn new(config: AvevaPiConfig, client: C) -> Self {
        Self {
            config,
            client,
            last_seen: Mutex::new(None),
        }
    }
}

#[async_trait]
impl<C: AvevaPiClient + 'static> StreamingSourceConnector for AvevaPiConnector<C> {
    fn kind(&self) -> &'static str {
        "aveva_pi"
    }

    async fn pull(&self, _opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError> {
        let since = self.last_seen.lock().ok().and_then(|g| *g);
        let observations = self
            .client
            .poll(&self.config.event_stream_web_id, since)
            .await?;
        if observations.is_empty() {
            return Err(ConnectorError::Empty);
        }
        let mut newest: Option<DateTime<Utc>> = None;
        let records = observations
            .into_iter()
            .map(|o| {
                if newest.map(|t| o.timestamp > t).unwrap_or(true) {
                    newest = Some(o.timestamp);
                }
                SourceRecord {
                    source_id: format!("{}:{}", self.config.event_stream_web_id, o.timestamp.timestamp_nanos_opt().unwrap_or(0)),
                    partition_key: Some(self.config.event_stream_web_id.clone()),
                    payload: serde_json::json!({
                        "timestamp": o.timestamp,
                        "value": o.value,
                        "good": o.good,
                        "questionable": o.questionable,
                        "substituted": o.substituted,
                    }),
                    event_time: o.timestamp,
                    metadata: serde_json::json!({
                        "web_id": self.config.event_stream_web_id,
                        "good": o.good,
                    }),
                }
            })
            .collect::<Vec<_>>();
        if let Some(ts) = newest {
            if let Ok(mut last) = self.last_seen.lock() {
                *last = Some(ts);
            }
        }
        Ok(records)
    }

    async fn checkpoint(&self, checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError> {
        if let Some(ts) = checkpoint
            .cursor
            .get("last_seen")
            .and_then(|v| v.as_str())
            .and_then(|s| DateTime::parse_from_rfc3339(s).ok())
        {
            if let Ok(mut last) = self.last_seen.lock() {
                *last = Some(ts.with_timezone(&Utc));
            }
        }
        Ok(())
    }

    async fn health(&self) -> ConnectorHealth {
        ConnectorHealth {
            status: ConnectorStatus::Healthy,
            backlog: 0,
            throughput_per_second: 0.0,
            last_pull_at: self.last_seen.lock().ok().and_then(|g| *g),
        }
    }
}

pub fn catalog_entry(stream: &StreamDefinition) -> ConnectorCatalogEntry {
    ConnectorCatalogEntry {
        connector_type: "aveva_pi".to_string(),
        direction: "source".to_string(),
        endpoint: stream.source_binding.endpoint.clone(),
        status: "healthy".to_string(),
        backlog: 0,
        throughput_per_second: 0.0,
        details: serde_json::json!({
            "format": stream.source_binding.format,
            "doc": "Aveva PI Web API source — polling-based",
        }),
    }
}
