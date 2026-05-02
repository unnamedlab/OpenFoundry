//! Hot buffer abstraction.
//!
//! The "hot" tier of the streaming storage stack is a publish-only view of
//! the most recent N seconds of events for each stream. It is backed by
//! either Apache Kafka (production) or NATS Core (lightweight dev / tests).
//! Cold-tier reads (Iceberg + Parquet) are handled separately by
//! `domain::archiver` and `handlers::streams::read_stream`.
//!
//! Two implementations live below this module:
//!   * [`nats::NatsHotBuffer`] — always available, used as the default
//!     and as the fallback when the Kafka backend is not compiled in.
//!   * [`kafka::KafkaHotBuffer`] — gated by the `kafka-rdkafka` feature,
//!     wraps `rdkafka::FutureProducer` + `AdminClient`.
//!
//! Both implement the [`HotBuffer`] trait so the rest of the service is
//! transport-agnostic.

use async_trait::async_trait;
use thiserror::Error;
use uuid::Uuid;

#[cfg(feature = "kafka-rdkafka")]
pub mod kafka;
pub mod nats;

#[cfg(feature = "kafka-rdkafka")]
pub use kafka::KafkaHotBuffer;
pub use nats::NatsHotBuffer;

/// Errors a hot buffer can surface to the REST layer.
#[derive(Debug, Error)]
pub enum HotBufferError {
    /// The backend is not reachable (broker down, not provisioned, etc.).
    #[error("hot buffer unavailable: {0}")]
    Unavailable(String),
    /// A specific publish / admin call failed at the transport layer.
    #[error("hot buffer transport error: {0}")]
    Transport(String),
}

/// Public contract every hot-buffer backend implements.
#[async_trait]
pub trait HotBuffer: Send + Sync + std::fmt::Debug {
    /// Stable identifier (`"kafka"`, `"nats"`, `"noop"`) used in logs and
    /// Prometheus labels.
    fn id(&self) -> &'static str;

    /// Make sure the topic backing `stream_id` exists.
    ///
    /// * For Kafka, this calls `AdminClient::create_topics` with
    ///   `partitions` and `replication.factor=1` (dev) so subsequent
    ///   producers don't auto-create with the broker default partition
    ///   count.
    /// * For NATS, this is a no-op: subjects are created on the first
    ///   publish.
    async fn ensure_topic(
        &self,
        stream_id: Uuid,
        partitions: i32,
    ) -> Result<(), HotBufferError>;

    /// Publish a single event payload.
    ///
    /// `key` is used for partitioning when the backend supports it
    /// (Kafka). NATS ignores it.
    async fn publish(
        &self,
        stream_id: Uuid,
        key: Option<&str>,
        payload: &[u8],
    ) -> Result<(), HotBufferError>;
}

/// Compose the conventional topic / subject name for a stream so every
/// backend uses the same naming scheme.
pub fn topic_for(stream_id: Uuid) -> String {
    format!("openfoundry.streams.{stream_id}")
}

/// Fallback hot buffer used when neither NATS nor Kafka are configured.
/// Logs and discards every publish so the REST control plane can still
/// boot in degraded mode (e.g. CI smoke tests against an in-memory
/// Postgres).
#[derive(Debug, Default, Clone, Copy)]
pub struct NoopHotBuffer;

#[async_trait]
impl HotBuffer for NoopHotBuffer {
    fn id(&self) -> &'static str {
        "noop"
    }

    async fn ensure_topic(
        &self,
        stream_id: Uuid,
        _partitions: i32,
    ) -> Result<(), HotBufferError> {
        tracing::debug!(stream_id = %stream_id, "noop hot buffer: ensure_topic");
        Ok(())
    }

    async fn publish(
        &self,
        stream_id: Uuid,
        _key: Option<&str>,
        payload: &[u8],
    ) -> Result<(), HotBufferError> {
        tracing::debug!(
            stream_id = %stream_id,
            bytes = payload.len(),
            "noop hot buffer: publish dropped"
        );
        Ok(())
    }
}
