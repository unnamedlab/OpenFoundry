//! Apache Kafka implementation of [`HotBuffer`] gated by the
//! `kafka-rdkafka` feature.
//!
//! Reuses the platform's [`event_bus_data::config::DataBusConfig`] so that
//! the hot-buffer producer shares the same SASL principal, idempotency
//! and `acks=all` defaults as the rest of the data plane.

use std::time::Duration;

use async_trait::async_trait;
use event_bus_data::config::{DataBusConfig, ServicePrincipal};
use rdkafka::admin::{AdminClient, AdminOptions, NewTopic, TopicReplication};
use rdkafka::client::DefaultClientContext;
use rdkafka::producer::{FutureProducer, FutureRecord};
use rdkafka::util::Timeout;
use uuid::Uuid;

use super::{HotBuffer, HotBufferError, topic_for};

/// Kafka hot buffer.
pub struct KafkaHotBuffer {
    producer: FutureProducer,
    admin: AdminClient<DefaultClientContext>,
    /// Hard cap for `producer.send` queueing + delivery. Aligned with the
    /// router-level Kafka backend.
    send_timeout: Duration,
}

impl std::fmt::Debug for KafkaHotBuffer {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("KafkaHotBuffer")
            .field("send_timeout", &self.send_timeout)
            .finish()
    }
}

impl KafkaHotBuffer {
    /// Build a hot buffer from a fully-resolved [`DataBusConfig`].
    pub fn new(config: DataBusConfig) -> Result<Self, HotBufferError> {
        let producer: FutureProducer =
            config.producer_config().create().map_err(|e| {
                HotBufferError::Unavailable(format!(
                    "could not build Kafka hot-buffer producer: {e}"
                ))
            })?;
        let admin: AdminClient<DefaultClientContext> =
            config.producer_config().create().map_err(|e| {
                HotBufferError::Unavailable(format!(
                    "could not build Kafka hot-buffer admin client: {e}"
                ))
            })?;
        Ok(Self {
            producer,
            admin,
            send_timeout: Duration::from_secs(30),
        })
    }

    /// Convenience constructor reading from `KAFKA_BOOTSTRAP_SERVERS` and
    /// optional `KAFKA_SASL_*`. Returns `Unavailable` when the bootstrap
    /// list is missing so `main.rs` can fall back to NATS.
    pub fn from_env(service_name: &str) -> Result<Self, HotBufferError> {
        let bootstrap = std::env::var("KAFKA_BOOTSTRAP_SERVERS").map_err(|_| {
            HotBufferError::Unavailable("KAFKA_BOOTSTRAP_SERVERS is not set".to_string())
        })?;
        let principal = match (
            std::env::var("KAFKA_SASL_USERNAME").ok(),
            std::env::var("KAFKA_SASL_PASSWORD").ok(),
        ) {
            (Some(_), Some(password)) => {
                ServicePrincipal::scram_sha_512(service_name.to_string(), password)
            }
            _ => ServicePrincipal::insecure_dev(service_name.to_string()),
        };
        Self::new(DataBusConfig::new(bootstrap, principal))
    }
}

#[async_trait]
impl HotBuffer for KafkaHotBuffer {
    fn id(&self) -> &'static str {
        "kafka"
    }

    async fn ensure_topic(
        &self,
        stream_id: Uuid,
        partitions: i32,
    ) -> Result<(), HotBufferError> {
        let topic_name = topic_for(stream_id);
        let new_topic = NewTopic::new(
            &topic_name,
            partitions.max(1),
            TopicReplication::Fixed(1),
        );
        let opts = AdminOptions::new().request_timeout(Some(Duration::from_secs(15)));
        let outcomes = self
            .admin
            .create_topics([&new_topic], &opts)
            .await
            .map_err(|e| HotBufferError::Transport(e.to_string()))?;
        for outcome in outcomes {
            match outcome {
                Ok(_) => {
                    tracing::info!(
                        stream_id = %stream_id,
                        topic = %topic_name,
                        partitions,
                        "kafka hot buffer: topic created"
                    );
                }
                // `TopicAlreadyExists` is the happy path for repeat calls.
                Err((_, rdkafka::types::RDKafkaErrorCode::TopicAlreadyExists)) => {
                    tracing::debug!(topic = %topic_name, "kafka hot buffer: topic already exists");
                }
                Err((name, code)) => {
                    return Err(HotBufferError::Transport(format!(
                        "create_topics({name}) failed: {code:?}"
                    )));
                }
            }
        }
        Ok(())
    }

    async fn publish(
        &self,
        stream_id: Uuid,
        key: Option<&str>,
        payload: &[u8],
    ) -> Result<(), HotBufferError> {
        let topic_name = topic_for(stream_id);
        let mut record = FutureRecord::to(&topic_name).payload(payload);
        if let Some(k) = key {
            record = record.key(k);
        }
        self.producer
            .send(record, Timeout::After(self.send_timeout))
            .await
            .map(|_| ())
            .map_err(|(err, _msg)| HotBufferError::Transport(err.to_string()))
    }
}
