//! Producer side of the data plane bus.

use std::time::Duration;

use async_trait::async_trait;
use rdkafka::error::KafkaError;
use rdkafka::producer::{FutureProducer, FutureRecord, Producer};
use rdkafka::util::Timeout;
use thiserror::Error;

use crate::config::DataBusConfig;
use crate::headers::OpenLineageHeaders;

#[derive(Debug, Error)]
pub enum PublishError {
    #[error("kafka client error: {0}")]
    Kafka(#[from] KafkaError),
    #[error("kafka delivery error: {0}")]
    Delivery(String),
    #[error("invalid record: {0}")]
    InvalidRecord(String),
}

/// At-least-once typed publisher into the data plane.
///
/// Implementations MUST:
///
/// - Block (asynchronously) until the broker has acknowledged the write
///   (`acks=all`), so callers can rely on durability before returning.
/// - Attach the supplied [`OpenLineageHeaders`] to every record.
/// - Never silently auto-create topics.
#[async_trait]
pub trait DataPublisher: Send + Sync {
    /// Publish a record to `topic` with an optional partition key.
    async fn publish(
        &self,
        topic: &str,
        key: Option<&[u8]>,
        payload: &[u8],
        lineage: &OpenLineageHeaders,
    ) -> Result<(), PublishError>;

    /// Flush any in-flight records. Useful on graceful shutdown.
    async fn flush(&self, timeout: Duration) -> Result<(), PublishError>;
}

/// Default `rdkafka`-backed implementation.
#[derive(Clone)]
pub struct KafkaPublisher {
    inner: FutureProducer,
}

impl KafkaPublisher {
    pub fn new(config: &DataBusConfig) -> Result<Self, PublishError> {
        let inner: FutureProducer = config.producer_config().create()?;
        Ok(Self { inner })
    }

    /// Construct from a pre-built `FutureProducer` (for tests / advanced use).
    pub fn from_producer(inner: FutureProducer) -> Self {
        Self { inner }
    }
}

#[async_trait]
impl DataPublisher for KafkaPublisher {
    async fn publish(
        &self,
        topic: &str,
        key: Option<&[u8]>,
        payload: &[u8],
        lineage: &OpenLineageHeaders,
    ) -> Result<(), PublishError> {
        if topic.is_empty() {
            return Err(PublishError::InvalidRecord("topic must not be empty".into()));
        }

        let headers = lineage.to_kafka_headers();
        let mut record = FutureRecord::to(topic).payload(payload).headers(headers);
        if let Some(k) = key {
            record = record.key(k);
        }

        // 30s upper bound for queueing+delivery.
        match self.inner.send(record, Timeout::After(Duration::from_secs(30))).await {
            Ok((partition, offset)) => {
                tracing::debug!(
                    topic,
                    partition,
                    offset,
                    namespace = %lineage.namespace,
                    job = %lineage.job_name,
                    run_id = %lineage.run_id,
                    "data plane record acknowledged"
                );
                Ok(())
            }
            Err((err, _msg)) => Err(PublishError::Delivery(err.to_string())),
        }
    }

    async fn flush(&self, timeout: Duration) -> Result<(), PublishError> {
        // `Producer::flush` is blocking; run it on the blocking pool so we do
        // not stall the async runtime.
        let producer = self.inner.clone();
        tokio::task::spawn_blocking(move || producer.flush(Timeout::After(timeout)))
            .await
            .map_err(|e| PublishError::Delivery(format!("flush task join error: {e}")))?
            .map_err(PublishError::Kafka)
    }
}
