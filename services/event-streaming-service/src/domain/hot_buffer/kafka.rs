//! Apache Kafka implementation of [`HotBuffer`] gated by the
//! `kafka-rdkafka` feature.
//!
//! Reuses the platform's [`event_bus_data::config::DataBusConfig`] so that
//! the hot-buffer producer shares the same SASL principal, idempotency
//! and `acks=all` defaults as the rest of the data plane.

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::Duration;

use async_trait::async_trait;
use event_bus_data::config::{DataBusConfig, ServicePrincipal};
use rdkafka::admin::{AdminClient, AdminOptions, NewTopic, TopicReplication};
use rdkafka::client::DefaultClientContext;
use rdkafka::producer::{FutureProducer, FutureRecord};
use rdkafka::util::Timeout;
use uuid::Uuid;

use super::{HotBuffer, HotBufferError, topic_for};
use crate::models::stream::{KafkaProducerSettings, StreamType};

/// Kafka hot buffer.
pub struct KafkaHotBuffer {
    /// Producer used when no per-stream tuning has been applied. Built
    /// with the platform defaults (idempotent producer, `acks=all`,
    /// `linger.ms=5`).
    producer: FutureProducer,
    admin: AdminClient<DefaultClientContext>,
    /// Hard cap for `producer.send` queueing + delivery. Aligned with the
    /// router-level Kafka backend.
    send_timeout: Duration,
    /// Base [`DataBusConfig`] kept around so per-stream producers can
    /// be re-derived with overlaid `linger.ms`/`batch.size`/
    /// `compression.type` settings when `apply_stream_type` is called.
    base_config: DataBusConfig,
    /// Per-stream producer overlays. Populated by
    /// [`KafkaHotBuffer::apply_stream_type`]; reads on the publish path
    /// fall back to `producer` when no override exists for `stream_id`.
    overrides: RwLock<HashMap<Uuid, FutureProducer>>,
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
        let producer: FutureProducer = config.producer_config().create().map_err(|e| {
            HotBufferError::Unavailable(format!("could not build Kafka hot-buffer producer: {e}"))
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
            base_config: config,
            overrides: RwLock::new(HashMap::new()),
        })
    }

    /// Build a tuned producer for a given Foundry stream type by
    /// overlaying the resolved [`KafkaProducerSettings`] on the base
    /// `DataBusConfig`. Pure function modulo `rdkafka` IO so it can be
    /// reused by tests.
    pub(crate) fn build_tuned_producer(
        config: &DataBusConfig,
        stream_type: StreamType,
        compression: bool,
    ) -> Result<FutureProducer, HotBufferError> {
        let settings = KafkaProducerSettings::for_stream(stream_type, compression);
        let mut client_config = config.producer_config();
        for (key, value) in settings.to_kafka_pairs() {
            client_config.set(key, &value);
        }
        client_config.create().map_err(|e| {
            HotBufferError::Unavailable(format!(
                "could not build tuned Kafka hot-buffer producer for {stream_type:?}: {e}"
            ))
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

    async fn ensure_topic(&self, stream_id: Uuid, partitions: i32) -> Result<(), HotBufferError> {
        let topic_name = topic_for(stream_id);
        let new_topic = NewTopic::new(&topic_name, partitions.max(1), TopicReplication::Fixed(1));
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
        // The map is read-mostly (writes only happen on
        // `apply_stream_type`) so the lock is uncontended on the hot
        // path. Cloning the `FutureProducer` is cheap (an Arc-backed
        // handle).
        let producer = self
            .overrides
            .read()
            .ok()
            .and_then(|map| map.get(&stream_id).cloned())
            .unwrap_or_else(|| self.producer.clone());
        producer
            .send(record, Timeout::After(self.send_timeout))
            .await
            .map(|_| ())
            .map_err(|(err, _msg)| HotBufferError::Transport(err.to_string()))
    }

    async fn apply_stream_type(
        &self,
        stream_id: Uuid,
        stream_type: StreamType,
        compression: bool,
    ) -> Result<(), HotBufferError> {
        // STANDARD with `compression=false` is the default — clear any
        // existing override so we fall back to the base producer rather
        // than leaking an over-tuned client.
        let is_default = matches!(stream_type, StreamType::Standard) && !compression;
        if is_default {
            if let Ok(mut map) = self.overrides.write() {
                map.remove(&stream_id);
            }
            return Ok(());
        }
        let tuned = Self::build_tuned_producer(&self.base_config, stream_type, compression)?;
        let mut map = self
            .overrides
            .write()
            .map_err(|_| HotBufferError::Transport("hot buffer overrides poisoned".to_string()))?;
        map.insert(stream_id, tuned);
        tracing::info!(
            stream_id = %stream_id,
            stream_type = stream_type.as_str(),
            compression,
            "kafka hot buffer: applied stream-type override"
        );
        Ok(())
    }
}
