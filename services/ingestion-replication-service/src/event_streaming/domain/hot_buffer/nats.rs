//! NATS Core implementation of [`HotBuffer`].
//!
//! Subjects are created lazily on the first publish, so [`ensure_topic`]
//! is a no-op. Publishes go through the same `async_nats::Client` used by
//! the gRPC routing facade (passed in at construction time so we don't
//! open a second TCP connection).

use async_nats::Client;
use async_trait::async_trait;
use bytes::Bytes;
use uuid::Uuid;

use super::{HotBuffer, HotBufferError, topic_for};

/// NATS Core hot buffer.
#[derive(Debug, Clone)]
pub struct NatsHotBuffer {
    client: Client,
}

impl NatsHotBuffer {
    /// Build a hot buffer that publishes to the given NATS server.
    pub async fn connect(url: &str) -> Result<Self, HotBufferError> {
        let client = async_nats::connect(url).await.map_err(|e| {
            HotBufferError::Unavailable(format!("could not connect to NATS at {url}: {e}"))
        })?;
        Ok(Self { client })
    }

    /// Wrap an already-connected client (used by tests and by the gRPC
    /// router which has already paid the connection cost).
    pub fn from_client(client: Client) -> Self {
        Self { client }
    }
}

#[async_trait]
impl HotBuffer for NatsHotBuffer {
    fn id(&self) -> &'static str {
        "nats"
    }

    async fn ensure_topic(&self, _stream_id: Uuid, _partitions: i32) -> Result<(), HotBufferError> {
        // NATS subjects are namespaces, not first-class entities. The
        // first publish materialises them with no setup cost.
        Ok(())
    }

    async fn publish(
        &self,
        stream_id: Uuid,
        _key: Option<&str>,
        payload: &[u8],
    ) -> Result<(), HotBufferError> {
        let subject = topic_for(stream_id);
        self.client
            .publish(subject, Bytes::copy_from_slice(payload))
            .await
            .map_err(|e| HotBufferError::Transport(e.to_string()))
    }
}
