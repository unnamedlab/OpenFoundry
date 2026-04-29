//! Stub Kafka backend.
//!
//! The real Kafka implementation backed by `rdkafka` is intentionally out of
//! scope for PR 1: it pulls in a C dependency (`librdkafka`) that we do not
//! want to require on every CI worker yet. Instead we ship a stub that fails
//! every call with [`BackendError::Unavailable`]. This keeps the routing
//! contract honest (callers see a real, mappable error) and lets PR 2 swap the
//! implementation without touching the router or the gRPC layer.

use async_trait::async_trait;

use crate::router::BackendId;

use super::{Backend, BackendError, Envelope, EnvelopeStream};

#[derive(Debug, Clone, Default)]
pub struct KafkaUnavailableBackend;

impl KafkaUnavailableBackend {
    pub fn new() -> Self {
        Self
    }
}

#[async_trait]
impl Backend for KafkaUnavailableBackend {
    fn id(&self) -> BackendId {
        BackendId::Kafka
    }

    async fn publish(&self, _envelope: Envelope) -> Result<(), BackendError> {
        Err(BackendError::Unavailable {
            backend: BackendId::Kafka,
            message:
                "Kafka backend is configured as a stub; enable the real rdkafka integration (PR 2) to use it"
                    .to_string(),
        })
    }

    async fn subscribe(&self, _pattern: &str) -> Result<EnvelopeStream, BackendError> {
        Err(BackendError::Unavailable {
            backend: BackendId::Kafka,
            message:
                "Kafka backend is configured as a stub; enable the real rdkafka integration (PR 2) to use it"
                    .to_string(),
        })
    }
}
