use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

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
    /// Map the profile flags to Kafka producer / topic-level settings.
    /// Returned as `(key, value)` pairs ready to be applied to either
    /// `rdkafka::ClientConfig` (producer) or `NewTopic::set` (topic).
    pub fn to_kafka_settings(&self) -> Vec<(&'static str, String)> {
        let mut out: Vec<(&'static str, String)> = Vec::new();
        if self.high_throughput {
            out.push(("linger.ms", "25".to_string()));
            out.push(("batch.size", "262144".to_string()));
        } else {
            out.push(("linger.ms", "5".to_string()));
            out.push(("batch.size", "32768".to_string()));
        }
        if self.compressed {
            out.push(("compression.type", "lz4".to_string()));
        } else {
            out.push(("compression.type", "none".to_string()));
        }
        out
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
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

fn default_compatibility() -> String {
    "BACKWARD".to_string()
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
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<StreamRow> for StreamDefinition {
    fn from(value: StreamRow) -> Self {
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
            created_at: value.created_at,
            updated_at: value.updated_at,
        }
    }
}
