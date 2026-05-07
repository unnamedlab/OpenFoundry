use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

/// Foundry-parity stream type tuning the underlying Kafka producer.
///
/// Mirrors the protobuf `openfoundry.streaming.streams.v1.StreamType`.
/// Persisted on `streaming_streams.stream_type` as text so the proto
/// enum and the SQL CHECK stay in lock-step without a custom Postgres
/// type.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum StreamType {
    #[default]
    Standard,
    HighThroughput,
    Compressed,
    HighThroughputCompressed,
}

impl StreamType {
    /// Parse the textual representation persisted in Postgres / surfaced
    /// over JSON. Treats unknown values as an error so callers get a
    /// `bad_request` rather than a silent fallback.
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "STANDARD" => Ok(Self::Standard),
            "HIGH_THROUGHPUT" => Ok(Self::HighThroughput),
            "COMPRESSED" => Ok(Self::Compressed),
            "HIGH_THROUGHPUT_COMPRESSED" => Ok(Self::HighThroughputCompressed),
            other => Err(format!("unknown stream_type: {other}")),
        }
    }

    /// Canonical textual form (matches the SQL CHECK and the proto enum
    /// names).
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Standard => "STANDARD",
            Self::HighThroughput => "HIGH_THROUGHPUT",
            Self::Compressed => "COMPRESSED",
            Self::HighThroughputCompressed => "HIGH_THROUGHPUT_COMPRESSED",
        }
    }

    pub fn is_high_throughput(self) -> bool {
        matches!(self, Self::HighThroughput | Self::HighThroughputCompressed)
    }

    pub fn is_compressed(self) -> bool {
        matches!(self, Self::Compressed | Self::HighThroughputCompressed)
    }
}

/// Streaming consistency contract. Maps directly onto the proto
/// `StreamConsistency` enum. Foundry's docs require `ingest` to be
/// AT_LEAST_ONCE for streaming sources; pipelines may opt into
/// EXACTLY_ONCE.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum StreamConsistency {
    #[default]
    AtLeastOnce,
    ExactlyOnce,
}

impl StreamConsistency {
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "AT_LEAST_ONCE" => Ok(Self::AtLeastOnce),
            "EXACTLY_ONCE" => Ok(Self::ExactlyOnce),
            other => Err(format!("unknown stream_consistency: {other}")),
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::AtLeastOnce => "AT_LEAST_ONCE",
            Self::ExactlyOnce => "EXACTLY_ONCE",
        }
    }
}

/// Resolved Kafka producer/topic settings for a given stream type.
///
/// `linger.ms`/`batch.size` come from the Foundry "high throughput"
/// recommendation; `lz4` is preferred over Snappy for its better
/// compression ratio at similar CPU cost.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct KafkaProducerSettings {
    pub linger_ms: u32,
    pub batch_size_bytes: u32,
    /// `None` means no explicit `compression.type` override (broker
    /// default).
    pub compression: Option<&'static str>,
}

impl KafkaProducerSettings {
    /// Resolve from the Foundry stream type. `compression` flag is
    /// honoured even on STANDARD/HIGH_THROUGHPUT for the rare case
    /// where an operator wants to force compression without taking the
    /// throughput-tuned linger/batch settings.
    pub fn for_stream(stream_type: StreamType, compression: bool) -> Self {
        let (linger_ms, batch_size_bytes) = if stream_type.is_high_throughput() {
            (20, 131_072)
        } else {
            (5, 32_768)
        };
        let compression = if stream_type.is_compressed() || compression {
            Some("lz4")
        } else {
            None
        };
        Self {
            linger_ms,
            batch_size_bytes,
            compression,
        }
    }

    /// Render as `(key, value)` pairs ready to be set on either
    /// `rdkafka::ClientConfig` (producer) or `NewTopic` overrides.
    pub fn to_kafka_pairs(&self) -> Vec<(&'static str, String)> {
        let mut out = vec![
            ("linger.ms", self.linger_ms.to_string()),
            ("batch.size", self.batch_size_bytes.to_string()),
        ];
        if let Some(codec) = self.compression {
            out.push(("compression.type", codec.to_string()));
        }
        out
    }
}

/// Operator-facing knobs that map to Kafka producer / topic tuning at
/// runtime. Stored as JSONB so we can extend the shape without further
/// migrations.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct StreamProfile {
    /// When true the Kafka producer is tuned for throughput
    /// (`linger.ms = 25`, `batch.size = 256 KiB`).
    #[serde(default)]
    pub high_throughput: bool,
    /// When true the Kafka producer enables `compression.type = lz4`.
    /// LZ4 is preferred over Snappy here for its better compression
    /// ratio at similar CPU cost.
    #[serde(default)]
    pub compressed: bool,
    /// Optional override for the topic partition count. When `None` the
    /// `StreamDefinition.partitions` field is used.
    #[serde(default)]
    pub partitions: Option<i32>,
}

impl StreamProfile {
    /// Derive a `StreamType` from the legacy `high_throughput`/
    /// `compressed` booleans so callers that still set the JSON profile
    /// pick up the new producer tuning.
    pub fn derive_stream_type(&self) -> StreamType {
        match (self.high_throughput, self.compressed) {
            (true, true) => StreamType::HighThroughputCompressed,
            (true, false) => StreamType::HighThroughput,
            (false, true) => StreamType::Compressed,
            (false, false) => StreamType::Standard,
        }
    }

    /// Map the profile flags to Kafka producer / topic-level settings.
    /// Kept for backwards compatibility — newer call-sites should call
    /// [`KafkaProducerSettings::for_stream`] directly with a
    /// `StreamType`.
    pub fn to_kafka_settings(&self) -> Vec<(&'static str, String)> {
        KafkaProducerSettings::for_stream(self.derive_stream_type(), false).to_kafka_pairs()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamField {
    pub name: String,
    pub data_type: String,
    pub nullable: bool,
    pub semantic_role: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamSchema {
    pub fields: Vec<StreamField>,
    pub primary_key: Option<String>,
    pub watermark_field: Option<String>,
}

impl Default for StreamSchema {
    fn default() -> Self {
        Self {
            fields: vec![
                StreamField {
                    name: "event_time".to_string(),
                    data_type: "timestamp".to_string(),
                    nullable: false,
                    semantic_role: "event_time".to_string(),
                },
                StreamField {
                    name: "customer_id".to_string(),
                    data_type: "string".to_string(),
                    nullable: false,
                    semantic_role: "join_key".to_string(),
                },
            ],
            primary_key: None,
            watermark_field: Some("event_time".to_string()),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectorBinding {
    pub connector_type: String,
    pub endpoint: String,
    pub format: String,
    pub config: Value,
}

impl Default for ConnectorBinding {
    fn default() -> Self {
        Self {
            connector_type: "kafka".to_string(),
            endpoint: "kafka://stream/orders".to_string(),
            format: "json".to_string(),
            config: serde_json::json!({ "compression": "snappy" }),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamDefinition {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub status: String,
    pub schema: StreamSchema,
    pub source_binding: ConnectorBinding,
    pub retention_hours: i32,
    /// Number of partitions the hot buffer (Kafka topic) is created with.
    pub partitions: i32,
    /// Publish-side delivery contract. One of `at-most-once`,
    /// `at-least-once`, `exactly-once`.
    pub consistency_guarantee: String,
    #[serde(default)]
    pub stream_profile: StreamProfile,
    /// Foundry-parity stream type controlling Kafka producer tuning.
    #[serde(default)]
    pub stream_type: StreamType,
    /// Whether the producer compresses message batches (lz4). Kept
    /// orthogonal to `stream_type` so an operator can compress a
    /// STANDARD stream without bumping `linger.ms`.
    #[serde(default)]
    pub compression: bool,
    /// Ingest-side consistency. Always `AT_LEAST_ONCE` per Foundry
    /// docs ("Streaming sources currently only support AT_LEAST_ONCE
    /// for extracts and exports"). The validator rejects EXACTLY_ONCE.
    #[serde(default)]
    pub ingest_consistency: StreamConsistency,
    /// Pipeline-side consistency, the contract Pipeline Builder maps to
    /// `execution.checkpointing.mode` for Flink-backed pipelines.
    #[serde(default)]
    pub pipeline_consistency: StreamConsistency,
    /// Default checkpoint cadence (ms) consumed by the runtime when the
    /// topology does not override it.
    #[serde(default = "default_checkpoint_interval_ms")]
    pub checkpoint_interval_ms: i32,
    /// Optional Avro schema (Bloque E2). When set the push path validates
    /// payloads against this schema using
    /// `event_bus_control::schema_registry::validate_payload`.
    #[serde(default)]
    pub schema_avro: Option<Value>,
    /// SHA-256 fingerprint of the canonicalised Avro schema text.
    #[serde(default)]
    pub schema_fingerprint: Option<String>,
    /// Confluent-style compatibility mode for the next schema evolution.
    #[serde(default = "default_compatibility")]
    pub schema_compatibility_mode: String,
    /// Optional dataset marking name (Bloque E3). When set, callers
    /// without a matching clearance get filtered out of `list_streams`
    /// and rejected on `get_stream` / `push_events`.
    #[serde(default)]
    pub default_marking: Option<String>,
    /// Foundry-parity classification: only `INGEST` streams may be
    /// reset (the docs are explicit — "resets are only available for
    /// ingest streams"). Defaults to `INGEST` so legacy rows behave
    /// like push targets.
    #[serde(default)]
    pub kind: super::stream_view::StreamKind,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

fn default_compatibility() -> String {
    "BACKWARD".to_string()
}

fn default_checkpoint_interval_ms() -> i32 {
    2_000
}

/// Operator-facing view of the new stream-config knobs introduced in
/// `proto/streaming/streams.proto::StreamConfig`. Surfaced by
/// `GET /v1/streams/{rid}/config` and accepted (with validation) by
/// `PUT /v1/streams/{rid}/config`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamConfig {
    pub stream_type: StreamType,
    pub compression: bool,
    pub partitions: i32,
    pub retention_ms: i64,
    pub ingest_consistency: StreamConsistency,
    pub pipeline_consistency: StreamConsistency,
    pub checkpoint_interval_ms: i32,
}

/// Patch shape for the `PUT /v1/streams/{rid}/config` endpoint. Every
/// field is optional so callers can change one knob at a time.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct UpdateStreamConfigRequest {
    pub stream_type: Option<StreamType>,
    pub compression: Option<bool>,
    pub partitions: Option<i32>,
    pub retention_ms: Option<i64>,
    pub ingest_consistency: Option<StreamConsistency>,
    pub pipeline_consistency: Option<StreamConsistency>,
    pub checkpoint_interval_ms: Option<i32>,
}

impl StreamDefinition {
    /// Project the persisted stream into the [`StreamConfig`] view.
    pub fn config_view(&self) -> StreamConfig {
        StreamConfig {
            stream_type: self.stream_type,
            compression: self.compression,
            partitions: self.partitions,
            // `retention_hours` is the source of truth for retention;
            // `retention_ms` is a derived view used by the proto.
            retention_ms: i64::from(self.retention_hours).saturating_mul(3_600_000),
            ingest_consistency: self.ingest_consistency,
            pipeline_consistency: self.pipeline_consistency,
            checkpoint_interval_ms: self.checkpoint_interval_ms,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamEventInput {
    pub payload: Value,
    pub event_time: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PushStreamEventsRequest {
    pub events: Vec<StreamEventInput>,
}

#[derive(Debug, Clone, Serialize)]
pub struct PushStreamEventsResponse {
    pub stream_id: Uuid,
    pub accepted_events: usize,
    pub dead_lettered_events: usize,
    pub first_sequence_no: Option<i64>,
    pub last_sequence_no: Option<i64>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateStreamRequest {
    pub name: String,
    pub description: Option<String>,
    pub status: Option<String>,
    pub schema: Option<StreamSchema>,
    pub source_binding: Option<ConnectorBinding>,
    pub retention_hours: Option<i32>,
    pub partitions: Option<i32>,
    pub consistency_guarantee: Option<String>,
    pub stream_profile: Option<StreamProfile>,
    #[serde(default)]
    pub schema_avro: Option<Value>,
    #[serde(default)]
    pub schema_compatibility_mode: Option<String>,
    #[serde(default)]
    pub default_marking: Option<String>,
    pub stream_type: Option<StreamType>,
    pub compression: Option<bool>,
    pub ingest_consistency: Option<StreamConsistency>,
    pub pipeline_consistency: Option<StreamConsistency>,
    pub checkpoint_interval_ms: Option<i32>,
    pub kind: Option<super::stream_view::StreamKind>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateStreamRequest {
    pub name: Option<String>,
    pub description: Option<String>,
    pub status: Option<String>,
    pub schema: Option<StreamSchema>,
    pub source_binding: Option<ConnectorBinding>,
    pub retention_hours: Option<i32>,
    pub partitions: Option<i32>,
    pub consistency_guarantee: Option<String>,
    pub stream_profile: Option<StreamProfile>,
    #[serde(default)]
    pub schema_avro: Option<Value>,
    #[serde(default)]
    pub schema_compatibility_mode: Option<String>,
    #[serde(default)]
    pub default_marking: Option<String>,
    pub stream_type: Option<StreamType>,
    pub compression: Option<bool>,
    pub ingest_consistency: Option<StreamConsistency>,
    pub pipeline_consistency: Option<StreamConsistency>,
    pub checkpoint_interval_ms: Option<i32>,
    pub kind: Option<super::stream_view::StreamKind>,
}

#[derive(Debug, Clone, FromRow)]
pub struct StreamRow {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub status: String,
    pub schema: SqlJson<StreamSchema>,
    pub source_binding: SqlJson<ConnectorBinding>,
    pub retention_hours: i32,
    pub partitions: i32,
    pub consistency_guarantee: String,
    pub stream_profile: SqlJson<StreamProfile>,
    pub schema_avro: Option<SqlJson<Value>>,
    pub schema_fingerprint: Option<String>,
    pub schema_compatibility_mode: String,
    pub default_marking: Option<String>,
    pub stream_type: String,
    pub compression: bool,
    pub ingest_consistency: String,
    pub pipeline_consistency: String,
    pub checkpoint_interval_ms: i32,
    pub kind: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<StreamRow> for StreamDefinition {
    fn from(value: StreamRow) -> Self {
        // The CHECK constraints in `20260504000001_stream_config.sql`
        // guarantee these values are well-formed; fall back to the
        // default if a future operator drops the constraint.
        let stream_type = StreamType::from_str(&value.stream_type).unwrap_or_default();
        let ingest_consistency =
            StreamConsistency::from_str(&value.ingest_consistency).unwrap_or_default();
        let pipeline_consistency =
            StreamConsistency::from_str(&value.pipeline_consistency).unwrap_or_default();
        let kind = super::stream_view::StreamKind::from_str(&value.kind).unwrap_or_default();
        Self {
            id: value.id,
            name: value.name,
            description: value.description,
            status: value.status,
            schema: value.schema.0,
            source_binding: value.source_binding.0,
            retention_hours: value.retention_hours,
            partitions: value.partitions,
            consistency_guarantee: value.consistency_guarantee,
            stream_profile: value.stream_profile.0,
            schema_avro: value.schema_avro.map(|v| v.0),
            schema_fingerprint: value.schema_fingerprint,
            schema_compatibility_mode: value.schema_compatibility_mode,
            default_marking: value.default_marking,
            stream_type,
            compression: value.compression,
            ingest_consistency,
            pipeline_consistency,
            checkpoint_interval_ms: value.checkpoint_interval_ms,
            kind,
            created_at: value.created_at,
            updated_at: value.updated_at,
        }
    }
}
