//! Runtime wiring for `ontology-indexer`.
//!
//! Behind the `runtime` feature so the pure decoder in [`crate`]
//! stays compilable without `librdkafka`. The actual consumer loop
//! is built handler-by-handler in follow-up PRs (mirrors the
//! S2.5.b / S3.2.d substrate-first pattern).

use crate::topics;

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

/// Prometheus metric names. Pinned so dashboards and alert rules
/// can reference them as constants (see
/// `infra/k8s/observability/prometheus-rules-indexer.yaml`).
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
