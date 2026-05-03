//! Bloque P5 — streaming-sync config catalogue.
//!
//! Surfaces the typed configuration shape every streaming source
//! kind expects so the data-connection wizard knows what fields to
//! render. The runner-side schemas live next to the connector
//! implementations in `event-streaming-service::domain::connectors`;
//! this catalogue mirrors them here so the connector-management
//! service can serve them without a cross-service hop.

use axum::{Json, response::IntoResponse};
use serde::Serialize;

#[derive(Debug, Serialize)]
pub struct StreamingSyncFieldDescriptor {
    pub name: &'static str,
    pub kind: &'static str, // "string", "int", "secret"
    pub required: bool,
    pub description: &'static str,
}

#[derive(Debug, Serialize)]
pub struct StreamingSourceContract {
    pub kind: &'static str,
    pub display_name: &'static str,
    pub description: &'static str,
    /// Whether this kind needs a Magritte agent or pulls directly.
    pub requires_agent: bool,
    pub config_fields: Vec<StreamingSyncFieldDescriptor>,
}

pub fn streaming_source_contracts() -> Vec<StreamingSourceContract> {
    use StreamingSyncFieldDescriptor as F;
    vec![
        StreamingSourceContract {
            kind: "streaming_kafka",
            display_name: "Apache Kafka",
            description: "Pull records from a Kafka topic via consumer-group offsets.",
            requires_agent: false,
            config_fields: vec![
                F { name: "bootstrap_servers", kind: "string", required: true, description: "Comma-separated host:port list." },
                F { name: "topic", kind: "string", required: true, description: "Topic the sync subscribes to." },
                F { name: "consumer_group", kind: "string", required: true, description: "Kafka consumer group id." },
                F { name: "auto_offset_reset", kind: "string", required: false, description: "earliest / latest." },
            ],
        },
        StreamingSourceContract {
            kind: "streaming_kinesis",
            display_name: "Amazon Kinesis",
            description: "Pull records from a Kinesis stream shard.",
            requires_agent: false,
            config_fields: vec![
                F { name: "stream_name", kind: "string", required: true, description: "Kinesis stream name." },
                F { name: "region", kind: "string", required: true, description: "AWS region." },
                F { name: "shard_iterator_type", kind: "string", required: false, description: "LATEST / TRIM_HORIZON." },
                F { name: "max_records_per_shard", kind: "int", required: false, description: "Soft cap per pull." },
            ],
        },
        StreamingSourceContract {
            kind: "streaming_sqs",
            display_name: "Amazon SQS",
            description: "Long-poll an SQS queue with explicit per-message ack.",
            requires_agent: false,
            config_fields: vec![
                F { name: "queue_url", kind: "string", required: true, description: "Full queue URL." },
                F { name: "region", kind: "string", required: true, description: "AWS region." },
                F { name: "wait_time_seconds", kind: "int", required: false, description: "Long-poll seconds (0..=20)." },
                F { name: "visibility_timeout_seconds", kind: "int", required: false, description: "Per-message visibility timeout." },
            ],
        },
        StreamingSourceContract {
            kind: "streaming_pubsub",
            display_name: "Google Cloud Pub/Sub",
            description: "REST-based pull + ack against a subscription.",
            requires_agent: false,
            config_fields: vec![
                F { name: "project_id", kind: "string", required: true, description: "GCP project id." },
                F { name: "subscription_id", kind: "string", required: true, description: "Subscription id." },
                F { name: "max_messages", kind: "int", required: false, description: "Soft cap per pull." },
                F { name: "ack_deadline_seconds", kind: "int", required: false, description: "Per-pull ack-deadline override." },
            ],
        },
        StreamingSourceContract {
            kind: "streaming_aveva_pi",
            display_name: "Aveva PI",
            description: "Poll the PI Web API for observation deltas.",
            requires_agent: false,
            config_fields: vec![
                F { name: "base_url", kind: "string", required: true, description: "PI Web API base URL." },
                F { name: "event_stream_web_id", kind: "string", required: true, description: "WebID of the event stream." },
                F { name: "poll_interval_ms", kind: "int", required: false, description: "Polling cadence." },
                F { name: "auth_header", kind: "secret", required: false, description: "Authorization header (Bearer / Basic)." },
            ],
        },
        StreamingSourceContract {
            kind: "streaming_external",
            display_name: "External transform (Magritte)",
            description: "Generic webhook hook for sources without a dedicated connector (ActiveMQ, Amazon SNS, IBM MQ, RabbitMQ, MQTT, Solace …).",
            requires_agent: true,
            config_fields: vec![
                F { name: "agent_label", kind: "string", required: true, description: "Free-form label for the catalogue." },
                F { name: "agent_token", kind: "secret", required: true, description: "Bearer token the agent uses to push records." },
                F { name: "protocol", kind: "string", required: true, description: "activemq | rabbitmq | mqtt | sns | ibm_mq | solace." },
            ],
        },
    ]
}

/// `GET /api/v1/data-connection/streaming-sources` — returns the
/// catalogue the data-connection wizard renders.
pub async fn list_streaming_sources() -> impl IntoResponse {
    Json(serde_json::json!({
        "data": streaming_source_contracts()
    }))
    .into_response()
}
