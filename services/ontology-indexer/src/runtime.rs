//! Runtime wiring for `ontology-indexer`.
//!
//! Behind the `runtime` feature so the pure decoder in [`crate`]
//! stays compilable without `librdkafka`.

use std::sync::Arc;
use std::time::Instant;

use event_bus_data::{
    CommitError, DataBusConfig, DataMessage, DataSubscriber, ServicePrincipal, SubscribeError,
};
use search_abstraction::{RepoError, SearchBackend};
use thiserror::Error;

use crate::{IndexAction, decode_object_changed, topics};

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
}

/// Errors that should keep the record uncommitted so Kafka can redeliver it
/// after the process restarts or the consumer group rebalances.
#[derive(Debug, Error)]
pub enum RuntimeError {
    #[error("required environment variable {0} is not set")]
    MissingEnv(&'static str),
    #[error("kafka subscribe/receive failed: {0}")]
    Subscribe(#[from] SubscribeError),
    #[error("kafka offset commit failed: {0}")]
    Commit(#[from] CommitError),
    #[error("search backend write failed: {0}")]
    Search(#[from] RepoError),
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
}
