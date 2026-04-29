//! Consumer side of the data plane bus.
//!
//! Provides at-least-once delivery with **explicit commits**: a record is not
//! considered processed until [`DataMessage::commit`] (or
//! [`DataSubscriber::commit_offsets`]) is called. Auto-commit is off in the
//! default consumer configuration.

use std::sync::Arc;

use async_trait::async_trait;
use rdkafka::consumer::{CommitMode, Consumer, StreamConsumer};
use rdkafka::error::KafkaError;
use rdkafka::message::Message;
use rdkafka::{Offset, TopicPartitionList};
use thiserror::Error;

use crate::config::DataBusConfig;
use crate::headers::OpenLineageHeaders;

#[derive(Debug, Error)]
pub enum SubscribeError {
    #[error("kafka client error: {0}")]
    Kafka(#[from] KafkaError),
    #[error("subscription error: {0}")]
    Subscription(String),
}

#[derive(Debug, Error)]
pub enum CommitError {
    #[error("kafka commit error: {0}")]
    Kafka(#[from] KafkaError),
}

/// One delivered record together with the metadata required to commit it
/// individually.
pub struct DataMessage {
    consumer: Arc<StreamConsumer>,
    topic: String,
    partition: i32,
    offset: i64,
    key: Option<Vec<u8>>,
    payload: Option<Vec<u8>>,
    lineage: Option<OpenLineageHeaders>,
}

impl DataMessage {
    pub fn topic(&self) -> &str {
        &self.topic
    }
    pub fn partition(&self) -> i32 {
        self.partition
    }
    pub fn offset(&self) -> i64 {
        self.offset
    }
    pub fn key(&self) -> Option<&[u8]> {
        self.key.as_deref()
    }
    pub fn payload(&self) -> Option<&[u8]> {
        self.payload.as_deref()
    }
    /// OpenLineage headers extracted from the record, if all required keys
    /// were present and well-formed.
    pub fn lineage(&self) -> Option<&OpenLineageHeaders> {
        self.lineage.as_ref()
    }

    /// Synchronously commit just this record's offset (`offset + 1`).
    pub fn commit(&self) -> Result<(), CommitError> {
        let mut tpl = TopicPartitionList::new();
        tpl.add_partition_offset(&self.topic, self.partition, Offset::Offset(self.offset + 1))
            .map_err(CommitError::Kafka)?;
        self.consumer.commit(&tpl, CommitMode::Sync)?;
        Ok(())
    }
}

impl std::fmt::Debug for DataMessage {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("DataMessage")
            .field("topic", &self.topic)
            .field("partition", &self.partition)
            .field("offset", &self.offset)
            .field("key_len", &self.key.as_ref().map(|k| k.len()))
            .field("payload_len", &self.payload.as_ref().map(|p| p.len()))
            .field("lineage", &self.lineage)
            .finish()
    }
}

/// At-least-once consumer trait.
#[async_trait]
pub trait DataSubscriber: Send + Sync {
    /// Subscribe to one or more topics. Topics must already exist (auto-create
    /// is disabled).
    fn subscribe(&self, topics: &[&str]) -> Result<(), SubscribeError>;

    /// Block until the next record is delivered.
    async fn recv(&self) -> Result<DataMessage, SubscribeError>;

    /// Commit the consumer position for the given message.
    fn commit(&self, message: &DataMessage) -> Result<(), CommitError>;

    /// Commit all currently-stored offsets at once.
    fn commit_offsets(&self) -> Result<(), CommitError>;
}

/// Default `rdkafka`-backed implementation.
#[derive(Clone)]
pub struct KafkaSubscriber {
    consumer: Arc<StreamConsumer>,
}

impl KafkaSubscriber {
    pub fn new(config: &DataBusConfig, group_id: &str) -> Result<Self, SubscribeError> {
        let consumer: StreamConsumer = config.consumer_config(group_id).create()?;
        Ok(Self {
            consumer: Arc::new(consumer),
        })
    }
}

#[async_trait]
impl DataSubscriber for KafkaSubscriber {
    fn subscribe(&self, topics: &[&str]) -> Result<(), SubscribeError> {
        if topics.is_empty() {
            return Err(SubscribeError::Subscription(
                "at least one topic required".into(),
            ));
        }
        self.consumer
            .subscribe(topics)
            .map_err(SubscribeError::Kafka)
    }

    async fn recv(&self) -> Result<DataMessage, SubscribeError> {
        let msg = self.consumer.recv().await?;
        let lineage = msg
            .headers()
            .and_then(OpenLineageHeaders::from_kafka_headers);
        Ok(DataMessage {
            consumer: Arc::clone(&self.consumer),
            topic: msg.topic().to_string(),
            partition: msg.partition(),
            offset: msg.offset(),
            key: msg.key().map(|k| k.to_vec()),
            payload: msg.payload().map(|p| p.to_vec()),
            lineage,
        })
    }

    fn commit(&self, message: &DataMessage) -> Result<(), CommitError> {
        message.commit()
    }

    fn commit_offsets(&self) -> Result<(), CommitError> {
        self.consumer
            .commit_consumer_state(CommitMode::Sync)
            .map_err(CommitError::Kafka)
    }
}
