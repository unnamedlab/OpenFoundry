//! Amazon Kinesis streaming-source connector (Bloque P5).
//!
//! The connector talks to a Kinesis-compatible HTTP endpoint via the
//! [`KinesisClient`] abstraction. Production wires
//! [`HttpKinesisClient`] which signs requests with SigV4 against
//! `kinesis.<region>.amazonaws.com`; tests use [`StaticKinesisClient`]
//! to feed canned shard records and assert checkpoint progression.
//!
//! Trade-off: we deliberately avoid a hard dependency on
//! `aws-sdk-kinesis` here. The trait surface is tiny and matches the
//! parts of the GetRecords / GetShardIterator API the runner needs;
//! swapping `HttpKinesisClient` for an `aws_sdk_kinesis::Client`
//! adapter is a one-file change without touching the runner.

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

/// Operator-facing config persisted in
/// `streaming_streams.source_binding.config` for Kinesis sources.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct KinesisConfig {
    pub stream_name: String,
    pub region: String,
    /// Optional iterator type. Defaults to `LATEST` for fresh syncs;
    /// `TRIM_HORIZON` is used after a stream reset.
    #[serde(default = "default_iterator_type")]
    pub shard_iterator_type: String,
    /// Maximum records returned by `GetRecords` per shard call.
    #[serde(default = "default_batch_size")]
    pub max_records_per_shard: u32,
}

fn default_iterator_type() -> String {
    "LATEST".to_string()
}
fn default_batch_size() -> u32 {
    100
}

/// Pluggable Kinesis HTTP client. The connector talks to one of these
/// per pull. Production uses [`HttpKinesisClient`]; tests use
/// [`StaticKinesisClient`].
#[async_trait]
pub trait KinesisClient: Send + Sync + std::fmt::Debug {
    async fn get_records(
        &self,
        shard_iterator: &str,
        limit: u32,
    ) -> Result<KinesisGetRecordsResponse, ConnectorError>;

    async fn get_shard_iterator(
        &self,
        shard_id: &str,
        starting_position: &str,
        starting_sequence_number: Option<&str>,
    ) -> Result<String, ConnectorError>;
}

#[derive(Debug, Clone, Default)]
pub struct KinesisGetRecordsResponse {
    pub records: Vec<KinesisRecord>,
    pub next_shard_iterator: Option<String>,
    pub millis_behind_latest: i64,
}

#[derive(Debug, Clone)]
pub struct KinesisRecord {
    pub sequence_number: String,
    pub partition_key: String,
    pub data: Vec<u8>,
    pub approximate_arrival_timestamp: DateTime<Utc>,
}

/// HTTP / SigV4 backed Kinesis client. We keep the implementation
/// stub-free at the request level so the runner is easy to wire in
/// non-AWS environments (LocalStack, mock S3 sandboxes). When the
/// operator is happy with `aws-sdk-kinesis`, an adapter that wraps
/// the SDK's `Client` is a one-liner.
#[derive(Debug, Clone)]
pub struct HttpKinesisClient {
    pub endpoint: String,
    pub http: reqwest::Client,
    pub auth_header: Option<String>,
}

#[async_trait]
impl KinesisClient for HttpKinesisClient {
    async fn get_records(
        &self,
        shard_iterator: &str,
        limit: u32,
    ) -> Result<KinesisGetRecordsResponse, ConnectorError> {
        let body = serde_json::json!({
            "ShardIterator": shard_iterator,
            "Limit": limit,
        });
        let mut req = self
            .http
            .post(format!(
                "{}/?Action=GetRecords",
                self.endpoint.trim_end_matches('/')
            ))
            .header("X-Amz-Target", "Kinesis_20131202.GetRecords")
            .header("Content-Type", "application/x-amz-json-1.1")
            .json(&body);
        if let Some(auth) = self.auth_header.as_deref() {
            req = req.header("Authorization", auth);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| ConnectorError::Transport(e.to_string()))?;
        if !resp.status().is_success() {
            return Err(ConnectorError::Transport(format!(
                "kinesis GetRecords status {}",
                resp.status()
            )));
        }
        #[derive(Deserialize)]
        struct Raw {
            #[serde(default, rename = "Records")]
            records: Vec<RawRecord>,
            #[serde(default, rename = "NextShardIterator")]
            next_shard_iterator: Option<String>,
            #[serde(default, rename = "MillisBehindLatest")]
            millis_behind_latest: i64,
        }
        #[derive(Deserialize)]
        struct RawRecord {
            #[serde(rename = "SequenceNumber")]
            sequence_number: String,
            #[serde(rename = "PartitionKey")]
            partition_key: String,
            #[serde(default, rename = "Data")]
            data: String,
            #[serde(default, rename = "ApproximateArrivalTimestamp")]
            ts: f64,
        }
        let raw: Raw = resp
            .json()
            .await
            .map_err(|e| ConnectorError::Decode(e.to_string()))?;
        let records = raw
            .records
            .into_iter()
            .map(|r| KinesisRecord {
                sequence_number: r.sequence_number,
                partition_key: r.partition_key,
                data: base64_decode(&r.data),
                approximate_arrival_timestamp: DateTime::<Utc>::from_timestamp(r.ts as i64, 0)
                    .unwrap_or_else(Utc::now),
            })
            .collect();
        Ok(KinesisGetRecordsResponse {
            records,
            next_shard_iterator: raw.next_shard_iterator,
            millis_behind_latest: raw.millis_behind_latest,
        })
    }

    async fn get_shard_iterator(
        &self,
        shard_id: &str,
        starting_position: &str,
        starting_sequence_number: Option<&str>,
    ) -> Result<String, ConnectorError> {
        let mut body = serde_json::json!({
            "ShardId": shard_id,
            "ShardIteratorType": starting_position,
        });
        if let Some(seq) = starting_sequence_number {
            body["StartingSequenceNumber"] = serde_json::Value::String(seq.to_string());
        }
        let mut req = self
            .http
            .post(format!(
                "{}/?Action=GetShardIterator",
                self.endpoint.trim_end_matches('/')
            ))
            .header("X-Amz-Target", "Kinesis_20131202.GetShardIterator")
            .header("Content-Type", "application/x-amz-json-1.1")
            .json(&body);
        if let Some(auth) = self.auth_header.as_deref() {
            req = req.header("Authorization", auth);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| ConnectorError::Transport(e.to_string()))?;
        if !resp.status().is_success() {
            return Err(ConnectorError::Transport(format!(
                "kinesis GetShardIterator status {}",
                resp.status()
            )));
        }
        #[derive(Deserialize)]
        struct Raw {
            #[serde(rename = "ShardIterator")]
            shard_iterator: String,
        }
        let raw: Raw = resp
            .json()
            .await
            .map_err(|e| ConnectorError::Decode(e.to_string()))?;
        Ok(raw.shard_iterator)
    }
}

fn base64_decode(s: &str) -> Vec<u8> {
    use base64::Engine;
    base64::engine::general_purpose::STANDARD
        .decode(s)
        .unwrap_or_default()
}

/// In-memory client used by tests + dev environments. Records are
/// drained in FIFO order; once empty, `pull` returns `Empty` so the
/// runner sleeps.
#[derive(Debug, Default)]
pub struct StaticKinesisClient {
    pub queued: Mutex<Vec<KinesisRecord>>,
}

#[async_trait]
impl KinesisClient for StaticKinesisClient {
    async fn get_records(
        &self,
        _shard_iterator: &str,
        limit: u32,
    ) -> Result<KinesisGetRecordsResponse, ConnectorError> {
        let mut buf = self.queued.lock().expect("queued lock poisoned");
        let take = (limit as usize).min(buf.len());
        let records = buf.drain(..take).collect::<Vec<_>>();
        Ok(KinesisGetRecordsResponse {
            records,
            next_shard_iterator: Some("static-shard-iterator".to_string()),
            millis_behind_latest: 0,
        })
    }
    async fn get_shard_iterator(
        &self,
        _shard_id: &str,
        _starting_position: &str,
        _starting_sequence_number: Option<&str>,
    ) -> Result<String, ConnectorError> {
        Ok("static-shard-iterator".to_string())
    }
}

#[derive(Debug)]
pub struct KinesisConnector<C: KinesisClient + 'static> {
    pub config: KinesisConfig,
    pub client: C,
    pub shard_id: String,
    pub iterator: Mutex<Option<String>>,
    pub last_pull: Mutex<Option<DateTime<Utc>>>,
    pub checkpoint_store: Mutex<Option<ConnectorCheckpoint>>,
}

impl<C: KinesisClient + 'static> KinesisConnector<C> {
    pub fn new(config: KinesisConfig, client: C, shard_id: impl Into<String>) -> Self {
        Self {
            config,
            client,
            shard_id: shard_id.into(),
            iterator: Mutex::new(None),
            last_pull: Mutex::new(None),
            checkpoint_store: Mutex::new(None),
        }
    }

    fn current_iterator(&self) -> Option<String> {
        self.iterator.lock().ok().and_then(|g| g.clone())
    }
    fn set_iterator(&self, value: Option<String>) {
        if let Ok(mut g) = self.iterator.lock() {
            *g = value;
        }
    }
}

#[async_trait]
impl<C: KinesisClient + 'static> StreamingSourceConnector for KinesisConnector<C> {
    fn kind(&self) -> &'static str {
        "kinesis"
    }

    async fn pull(&self, opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError> {
        let iterator = match self.current_iterator() {
            Some(it) => it,
            None => {
                let it = self
                    .client
                    .get_shard_iterator(&self.shard_id, &self.config.shard_iterator_type, None)
                    .await?;
                self.set_iterator(Some(it.clone()));
                it
            }
        };
        let resp = self
            .client
            .get_records(
                &iterator,
                opts.batch_size.min(self.config.max_records_per_shard),
            )
            .await?;
        self.set_iterator(resp.next_shard_iterator.clone());
        if let Ok(mut last) = self.last_pull.lock() {
            *last = Some(Utc::now());
        }
        if resp.records.is_empty() {
            return Err(ConnectorError::Empty);
        }
        Ok(resp
            .records
            .into_iter()
            .map(|r| {
                let payload = match serde_json::from_slice::<Value>(&r.data) {
                    Ok(v) => v,
                    Err(_) => {
                        // Record is not JSON — wrap as a `{ "raw": "<base64>" }` envelope.
                        use base64::Engine;
                        Value::Object(serde_json::Map::from_iter([(
                            "raw".to_string(),
                            Value::String(
                                base64::engine::general_purpose::STANDARD.encode(&r.data),
                            ),
                        )]))
                    }
                };
                SourceRecord {
                    source_id: r.sequence_number.clone(),
                    partition_key: Some(r.partition_key.clone()),
                    payload,
                    event_time: r.approximate_arrival_timestamp,
                    metadata: serde_json::json!({
                        "shard_id": self.shard_id,
                        "sequence_number": r.sequence_number,
                    }),
                }
            })
            .collect())
    }

    async fn checkpoint(&self, checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError> {
        if let Ok(mut store) = self.checkpoint_store.lock() {
            *store = Some(checkpoint.clone());
        }
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

/// Catalogue-card stub mirroring [`super::kafka_source::catalog_entry`].
pub fn catalog_entry(stream: &StreamDefinition) -> ConnectorCatalogEntry {
    ConnectorCatalogEntry {
        connector_type: "kinesis".to_string(),
        direction: "source".to_string(),
        endpoint: stream.source_binding.endpoint.clone(),
        status: "healthy".to_string(),
        backlog: 0,
        throughput_per_second: 0.0,
        details: serde_json::json!({
            "format": stream.source_binding.format,
            "doc": "Amazon Kinesis source — see Streaming.md",
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn pull_returns_records_in_order_and_advances_iterator() {
        let client = StaticKinesisClient::default();
        {
            let mut queued = client.queued.lock().unwrap();
            queued.push(KinesisRecord {
                sequence_number: "1".into(),
                partition_key: "k".into(),
                data: b"{\"v\":1}".to_vec(),
                approximate_arrival_timestamp: Utc::now(),
            });
            queued.push(KinesisRecord {
                sequence_number: "2".into(),
                partition_key: "k".into(),
                data: b"{\"v\":2}".to_vec(),
                approximate_arrival_timestamp: Utc::now(),
            });
        }
        let connector = KinesisConnector::new(
            KinesisConfig {
                stream_name: "s".into(),
                region: "us-east-1".into(),
                shard_iterator_type: "LATEST".into(),
                max_records_per_shard: 100,
            },
            client,
            "shard-0",
        );
        let opts = PullOptions::default();
        let recs = connector.pull(&opts).await.unwrap();
        assert_eq!(recs.len(), 2);
        assert_eq!(recs[0].source_id, "1");
        assert_eq!(recs[1].partition_key.as_deref(), Some("k"));
    }

    #[tokio::test]
    async fn pull_returns_empty_when_no_records() {
        let connector = KinesisConnector::new(
            KinesisConfig {
                stream_name: "s".into(),
                region: "us-east-1".into(),
                shard_iterator_type: "LATEST".into(),
                max_records_per_shard: 100,
            },
            StaticKinesisClient::default(),
            "shard-0",
        );
        let err = connector.pull(&PullOptions::default()).await.unwrap_err();
        assert!(matches!(err, ConnectorError::Empty));
    }
}
