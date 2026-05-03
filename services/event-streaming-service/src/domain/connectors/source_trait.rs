//! Foundry-parity streaming source connector trait.
//!
//! Bloque P5 — every external source (Kafka, Kinesis, SQS, Pub/Sub,
//! Aveva PI, Magritte external transform) implements
//! [`StreamingSourceConnector`] so the streaming-sync runner can pull
//! records uniformly. Each connector also owns its own offset
//! checkpointing so a connector restart does not replay or lose data.
//!
//! The trait is intentionally narrow: pull a batch, ack/checkpoint,
//! describe yourself for the catalogue, surface backpressure /
//! liveness signals. The runner is responsible for committing batches
//! to the hot buffer and progressing the cold-tier archiver.

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use thiserror::Error;
use uuid::Uuid;

/// Errors a connector may surface to the runner.
#[derive(Debug, Error)]
pub enum ConnectorError {
    /// Source is reachable but returned no records this round.
    #[error("source returned no records")]
    Empty,
    /// Authentication / authorization failure (401/403).
    #[error("auth: {0}")]
    Auth(String),
    /// Source is unreachable (network, DNS, connection refused).
    #[error("unavailable: {0}")]
    Unavailable(String),
    /// Source rejected the call with a 4xx other than auth.
    #[error("client error: {0}")]
    Client(String),
    /// Source returned a 5xx or surface-level transport error.
    #[error("transport: {0}")]
    Transport(String),
    /// Schema inference / payload decoding failed.
    #[error("decode: {0}")]
    Decode(String),
}

/// One record pulled from an external source.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SourceRecord {
    /// Source-specific record identifier (Kafka offset, Kinesis
    /// sequence number, SQS message id, PubSub ack id, etc.). The
    /// runner threads this back through [`StreamingSourceConnector::ack`].
    pub source_id: String,
    /// Sortable key the source provided (Kafka key, Kinesis partition
    /// key, SQS group id, PubSub ordering key). Used by the hot
    /// buffer's per-partition ordering guarantees.
    pub partition_key: Option<String>,
    /// Decoded JSON payload. Connectors are responsible for turning
    /// raw bytes into JSON; the runner expects an object.
    pub payload: Value,
    /// Wall-clock event time (RFC3339 in source) when available;
    /// otherwise the connector's read time.
    pub event_time: DateTime<Utc>,
    /// Free-form metadata the connector wants to surface (Kinesis
    /// shard id, SQS receipt handle, PubSub message id …).
    #[serde(default)]
    pub metadata: Value,
}

/// Configuration shared by every streaming sync.
///
/// Connectors layer their own typed config on top — see
/// [`super::kinesis::KinesisConfig`] etc. — but the runner reads
/// these knobs without caring about the concrete shape.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamingSyncConfig {
    /// Stream RID this sync writes into. Must be a Foundry INGEST
    /// stream with a current view (see Bloque P2).
    pub target_stream_rid: String,
    /// Maximum records pulled per `pull()` call. Connectors may
    /// return fewer.
    pub batch_size: u32,
    /// Delay between consecutive polls when `pull()` returns no
    /// records. Source-level long-poll waits (e.g. SQS 20s
    /// `WaitTimeSeconds`) take precedence; this is a fallback.
    pub poll_interval_ms: u64,
    /// When `true` the runner samples the first batch, infers the
    /// JSON schema and posts it to `event-streaming-service` so the
    /// stream's Avro schema matches the source. Defaults to `false`.
    #[serde(default)]
    pub schema_inference: bool,
}

impl Default for StreamingSyncConfig {
    fn default() -> Self {
        Self {
            target_stream_rid: String::new(),
            batch_size: 100,
            poll_interval_ms: 1_000,
            schema_inference: false,
        }
    }
}

/// Tunable knobs for [`StreamingSourceConnector::pull`].
#[derive(Debug, Clone)]
pub struct PullOptions {
    pub batch_size: u32,
    /// Hard timeout the runner is willing to wait on a single pull.
    pub max_wait_ms: u64,
}

impl Default for PullOptions {
    fn default() -> Self {
        Self {
            batch_size: 100,
            max_wait_ms: 5_000,
        }
    }
}

/// Source-level checkpoint. Connectors persist this opaque blob in
/// `streaming_connector_checkpoints` so the runner can resume at the
/// last acknowledged offset after a restart.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectorCheckpoint {
    pub connector_kind: String,
    pub stream_id: Uuid,
    /// Source-specific opaque cursor. For Kafka: a JSON map of
    /// `{ partition: offset }`. For Kinesis: the latest shard
    /// sequence number per shard. For SQS: the receipt handles still
    /// in flight. For PubSub: ack ids.
    pub cursor: Value,
    pub updated_at: DateTime<Utc>,
}

/// Liveness / health snapshot the catalogue surfaces.
#[derive(Debug, Clone, Default, Serialize)]
pub struct ConnectorHealth {
    pub status: ConnectorStatus,
    pub backlog: i64,
    pub throughput_per_second: f64,
    pub last_pull_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ConnectorStatus {
    #[default]
    Healthy,
    Degraded,
    Unreachable,
    Disabled,
}

/// Pull-side contract every streaming source implements.
#[async_trait]
pub trait StreamingSourceConnector: Send + Sync + std::fmt::Debug {
    /// Stable identifier (`"kafka"`, `"kinesis"`, …) used in metrics
    /// labels and the catalogue.
    fn kind(&self) -> &'static str;

    /// Attempt to read a batch of records. Returns
    /// [`ConnectorError::Empty`] when the source is healthy but had
    /// nothing to deliver — the runner uses that to decide whether to
    /// sleep `poll_interval_ms`.
    async fn pull(&self, opts: &PullOptions) -> Result<Vec<SourceRecord>, ConnectorError>;

    /// Persist a checkpoint after the runner has successfully
    /// committed a batch to the hot buffer.
    async fn checkpoint(&self, checkpoint: &ConnectorCheckpoint) -> Result<(), ConnectorError>;

    /// Acknowledge a single record at the source (SQS delete,
    /// PubSub ack, etc.). For at-least-once sources where there is
    /// no per-record ack (Kafka, Kinesis), the default is a no-op.
    async fn ack(&self, _record: &SourceRecord) -> Result<(), ConnectorError> {
        Ok(())
    }

    /// Lightweight probe used by the catalogue card.
    async fn health(&self) -> ConnectorHealth {
        ConnectorHealth::default()
    }
}
