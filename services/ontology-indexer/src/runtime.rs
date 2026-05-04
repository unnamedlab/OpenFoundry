//! Runtime wiring for `ontology-indexer`.
//!
//! Behind the `runtime` feature so the pure decoder in [`crate`]
//! stays compilable without `librdkafka`.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Instant;

use chrono::Utc;
use event_bus_data::{
    CommitError, DataBusConfig, DataMessage, DataSubscriber, ServicePrincipal, SubscribeError,
};
use prometheus::{
    Encoder, HistogramVec, IntCounterVec, Opts, Registry, TextEncoder, histogram_opts,
};
use search_abstraction::{RepoError, SearchBackend};
use thiserror::Error;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

use crate::{IndexAction, decode_object_changed, schema, topics};

/// Kafka consumer group used by every replica of the indexer. Pinned
/// here so a typo across replicas does not silently fork the
/// rebalance state.
pub const CONSUMER_GROUP: &str = "ontology-indexer";

/// Topics the indexer subscribes to on startup.
pub const SUBSCRIBE_TOPICS: &[&str] = &[
    topics::ONTOLOGY_OBJECT_CHANGED_V1,
    topics::ONTOLOGY_ACTION_APPLIED_V1,
    topics::ONTOLOGY_REINDEX_V1,
];

/// Outcome of one consumed Kafka record.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RecordOutcome {
    /// Object was indexed or re-indexed.
    Indexed,
    /// Object was deleted from the search backend.
    Deleted,
    /// Record was malformed and deliberately skipped after logging.
    DecodeError,
    /// Record carried no payload and was skipped after logging.
    EmptyPayload,
    /// Record on `ontology.reindex.v1` failed JSON-Schema validation
    /// against the contract pinned in
    /// `services/ontology-indexer/schemas/ontology.reindex.v1.json`
    /// (Tarea 4.4). Skipped after logging so a single poisoned
    /// producer batch cannot stall the consumer group; surfaced as
    /// its own metric label so an alert can DLQ-route it.
    SchemaInvalid,
}

impl RecordOutcome {
    fn as_metric_label(self) -> &'static str {
        match self {
            RecordOutcome::Indexed => "indexed",
            RecordOutcome::Deleted => "deleted",
            RecordOutcome::DecodeError => "decode_error",
            RecordOutcome::EmptyPayload => "empty_payload",
            RecordOutcome::SchemaInvalid => "schema_invalid",
        }
    }
}

/// Errors that should keep the record uncommitted so Kafka can redeliver it
/// after the process restarts or the consumer group rebalances.
#[derive(Debug, Error)]
pub enum RuntimeError {
    #[error("required environment variable {0} is not set")]
    MissingEnv(&'static str),
    #[error("invalid environment variable {key}={value:?}: {reason}")]
    InvalidEnv {
        key: &'static str,
        value: String,
        reason: &'static str,
    },
    #[error("kafka subscribe/receive failed: {0}")]
    Subscribe(#[from] SubscribeError),
    #[error("kafka offset commit failed: {0}")]
    Commit(#[from] CommitError),
    #[error("search backend write failed: {0}")]
    Search(#[from] RepoError),
    #[error("ontology.reindex.v1 schema failed to compile at startup: {0}")]
    SchemaCompile(#[from] schema::SchemaError),
}

/// Build the Kafka data-bus config from the standard OpenFoundry env vars.
pub fn data_bus_config_from_env(service_name: &str) -> Result<DataBusConfig, RuntimeError> {
    let brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS")
        .map_err(|_| RuntimeError::MissingEnv("KAFKA_BOOTSTRAP_SERVERS"))?;
    let service = non_empty_env("KAFKA_SASL_USERNAME")
        .or_else(|| non_empty_env("KAFKA_CLIENT_ID"))
        .unwrap_or_else(|| service_name.to_string());

    let mut principal = match non_empty_env("KAFKA_SASL_PASSWORD") {
        Some(password) => ServicePrincipal::scram_sha_512(service, password),
        None => ServicePrincipal::insecure_dev(service),
    };

    if let Some(mechanism) = non_empty_env("KAFKA_SASL_MECHANISM") {
        principal.mechanism = mechanism;
    }
    if let Some(protocol) = non_empty_env("KAFKA_SECURITY_PROTOCOL") {
        principal.security_protocol = protocol;
    }

    Ok(DataBusConfig::new(brokers, principal))
}

fn non_empty_env(key: &'static str) -> Option<String> {
    std::env::var(key).ok().and_then(|value| {
        let trimmed = value.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed.to_string())
        }
    })
}

/// Subscribe and run the at-least-once consumer loop.
pub async fn run<S>(subscriber: S, backend: Arc<dyn SearchBackend>) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    run_with_metrics(subscriber, backend, None).await
}

pub async fn run_with_metrics<S>(
    subscriber: S,
    backend: Arc<dyn SearchBackend>,
    metrics: Option<Arc<RuntimeMetrics>>,
) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    // Compile the `ontology.reindex.v1` JSON Schema once at startup
    // so a malformed artifact (e.g. a botched Helm-time edit) fails
    // the readiness probe instead of the first batch (Tarea 4.4).
    schema::ensure_compiled()?;

    subscriber.subscribe(SUBSCRIBE_TOPICS)?;
    tracing::info!(
        group = CONSUMER_GROUP,
        topics = ?SUBSCRIBE_TOPICS,
        "ontology-indexer consumer loop started"
    );

    loop {
        let message = subscriber.recv().await?;
        let outcome = process_message(backend.as_ref(), &message).await?;
        subscriber.commit(&message)?;
        if let Some(metrics) = metrics.as_deref() {
            metrics.record_processed(&message, outcome);
        }
        tracing::debug!(
            topic = message.topic(),
            partition = message.partition(),
            offset = message.offset(),
            ?outcome,
            "ontology-indexer committed record"
        );
    }
}

/// Decode and apply one record. Backend failures are returned so the caller
/// does not commit the offset.
pub async fn process_message(
    backend: &dyn SearchBackend,
    message: &DataMessage,
) -> Result<RecordOutcome, RuntimeError> {
    let Some(payload) = message.payload() else {
        tracing::warn!(
            topic = message.topic(),
            partition = message.partition(),
            offset = message.offset(),
            "ontology-indexer skipping record without payload"
        );
        return Ok(RecordOutcome::EmptyPayload);
    };

    // Schema gate for the `ontology.reindex.v1` topic only — the
    // live `object.changed.v1` / `action.applied.v1` topics are
    // already produced via the EventRouter SMT against schemas
    // registered elsewhere; gating them here is out of scope for
    // Tarea 4.4 and would risk dropping live writes during the
    // cut-over.
    if message.topic() == topics::ONTOLOGY_REINDEX_V1 {
        if let Err(error) = schema::validate_bytes(payload) {
            tracing::warn!(
                topic = message.topic(),
                partition = message.partition(),
                offset = message.offset(),
                %error,
                "ontology-indexer skipping reindex record that violates ontology.reindex.v1 schema"
            );
            return Ok(RecordOutcome::SchemaInvalid);
        }
    }

    let action = match decode_object_changed(payload) {
        Ok(action) => action,
        Err(error) => {
            tracing::warn!(
                topic = message.topic(),
                partition = message.partition(),
                offset = message.offset(),
                %error,
                "ontology-indexer skipping malformed record"
            );
            return Ok(RecordOutcome::DecodeError);
        }
    };

    let started = Instant::now();
    match action {
        IndexAction::Index { key, doc } => {
            backend.index(doc).await?;
            tracing::info!(
                tenant = %key.tenant.0,
                object_id = %key.id.0,
                version = key.version,
                elapsed_ms = started.elapsed().as_millis(),
                "ontology object indexed"
            );
            Ok(RecordOutcome::Indexed)
        }
        IndexAction::Delete { key } => {
            backend.delete(&key.tenant, &key.id).await?;
            tracing::info!(
                tenant = %key.tenant.0,
                object_id = %key.id.0,
                version = key.version,
                elapsed_ms = started.elapsed().as_millis(),
                "ontology object deleted from search index"
            );
            Ok(RecordOutcome::Deleted)
        }
    }
}

/// Prometheus metric names. Pinned so dashboards and alert rules
/// can reference them as constants (see
/// `infra/k8s/platform/manifests/observability/prometheus-rules-indexer.yaml`).
pub mod metrics {
    /// Histogram (seconds): gap between `event.created_at` (Kafka
    /// record timestamp) and `index.applied_at` (post-`index()`
    /// timestamp). SLO P99 < 5s.
    pub const INDEXER_LAG_SECONDS: &str = "ontology_indexer_lag_seconds";

    /// Counter: total records consumed, labelled by topic + outcome
    /// (`indexed`, `deleted`, `skipped_stale`, `decode_error`).
    pub const INDEXER_RECORDS_TOTAL: &str = "ontology_indexer_records_total";

    /// Gauge: consumer-side rdkafka lag, labelled by topic+partition.
    /// Scraped from the rdkafka stats callback.
    pub const INDEXER_KAFKA_LAG: &str = "ontology_indexer_kafka_lag_records";
}

#[derive(Clone)]
pub struct RuntimeMetrics {
    registry: Arc<Registry>,
    indexer_lag_seconds: HistogramVec,
    indexer_records_total: IntCounterVec,
}

impl Default for RuntimeMetrics {
    fn default() -> Self {
        Self::new()
    }
}

impl RuntimeMetrics {
    pub fn new() -> Self {
        let registry = Arc::new(Registry::new());
        let indexer_lag_seconds = HistogramVec::new(
            histogram_opts!(
                metrics::INDEXER_LAG_SECONDS,
                "Seconds between Kafka record timestamp and successful search backend commit.",
                vec![0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0]
            ),
            &["topic", "outcome"],
        )
        .expect("valid ontology_indexer_lag_seconds metric");
        let indexer_records_total = IntCounterVec::new(
            Opts::new(
                metrics::INDEXER_RECORDS_TOTAL,
                "Ontology indexer records committed by topic and outcome.",
            ),
            &["topic", "outcome"],
        )
        .expect("valid ontology_indexer_records_total metric");

        registry
            .register(Box::new(indexer_lag_seconds.clone()))
            .expect("register ontology_indexer_lag_seconds");
        registry
            .register(Box::new(indexer_records_total.clone()))
            .expect("register ontology_indexer_records_total");

        let metrics = Self {
            registry,
            indexer_lag_seconds,
            indexer_records_total,
        };
        metrics.prime();
        metrics
    }

    fn prime(&self) {
        for topic in SUBSCRIBE_TOPICS {
            for outcome in [
                RecordOutcome::Indexed,
                RecordOutcome::Deleted,
                RecordOutcome::DecodeError,
                RecordOutcome::EmptyPayload,
                RecordOutcome::SchemaInvalid,
            ] {
                let labels = &[*topic, outcome.as_metric_label()];
                self.indexer_records_total.with_label_values(labels);
                self.indexer_lag_seconds.with_label_values(labels);
            }
        }
    }

    fn record_processed(&self, message: &DataMessage, outcome: RecordOutcome) {
        let outcome_label = outcome.as_metric_label();
        self.indexer_records_total
            .with_label_values(&[message.topic(), outcome_label])
            .inc();
        if let Some(timestamp_millis) = message.timestamp_millis() {
            let now_millis = Utc::now().timestamp_millis();
            let lag_seconds = (now_millis.saturating_sub(timestamp_millis).max(0) as f64) / 1_000.0;
            self.indexer_lag_seconds
                .with_label_values(&[message.topic(), outcome_label])
                .observe(lag_seconds);
        }
    }

    pub fn render(&self) -> Result<String, prometheus::Error> {
        let mut buf = Vec::new();
        TextEncoder::new().encode(&self.registry.gather(), &mut buf)?;
        Ok(String::from_utf8(buf).unwrap_or_default())
    }
}

pub fn metrics_addr_from_env(default_port: u16) -> Result<SocketAddr, RuntimeError> {
    let value = std::env::var("METRICS_ADDR").unwrap_or_else(|_| format!("0.0.0.0:{default_port}"));
    value
        .parse::<SocketAddr>()
        .map_err(|_| RuntimeError::InvalidEnv {
            key: "METRICS_ADDR",
            value,
            reason: "expected socket address, for example 0.0.0.0:9090",
        })
}

pub async fn serve_metrics(metrics: Arc<RuntimeMetrics>, addr: SocketAddr) -> std::io::Result<()> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    loop {
        let (mut stream, _) = listener.accept().await?;
        let metrics = Arc::clone(&metrics);
        tokio::spawn(async move {
            let mut buf = [0_u8; 1024];
            let read =
                tokio::time::timeout(std::time::Duration::from_secs(2), stream.read(&mut buf))
                    .await
                    .ok()
                    .and_then(Result::ok)
                    .unwrap_or(0);
            let request = String::from_utf8_lossy(&buf[..read]);
            let path = request.split_whitespace().nth(1).unwrap_or("/");
            let (status, content_type, body) = match path {
                "/health" | "/healthz" => ("200 OK", "text/plain; charset=utf-8", "ok\n".into()),
                "/metrics" => match metrics.render() {
                    Ok(body) => ("200 OK", "text/plain; version=0.0.4", body),
                    Err(error) => (
                        "500 Internal Server Error",
                        "text/plain; charset=utf-8",
                        format!("failed to render metrics: {error}\n"),
                    ),
                },
                _ => (
                    "404 Not Found",
                    "text/plain; charset=utf-8",
                    "not found\n".into(),
                ),
            };
            let response = format!(
                "HTTP/1.1 {status}\r\ncontent-type: {content_type}\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = stream.write_all(response.as_bytes()).await;
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn subscribe_topics_pinned() {
        assert_eq!(SUBSCRIBE_TOPICS.len(), 3);
        assert!(SUBSCRIBE_TOPICS.contains(&"ontology.object.changed.v1"));
        assert!(SUBSCRIBE_TOPICS.contains(&"ontology.action.applied.v1"));
        assert!(SUBSCRIBE_TOPICS.contains(&"ontology.reindex.v1"));
    }

    #[test]
    fn runtime_metrics_render_prometheus_text() {
        let metrics = RuntimeMetrics::new();
        let body = metrics.render().unwrap();
        assert!(body.contains(metrics::INDEXER_RECORDS_TOTAL));
        assert!(body.contains(metrics::INDEXER_LAG_SECONDS));
    }
}
