//! Real NATS backend.
//!
//! Uses the async-nats core client. JetStream is available via `libs/event-bus`
//! for callers that need durable streams; the router itself stays on the
//! lightweight publish/subscribe path because subject wildcards (`*`, `>`) are
//! natively supported and matches the routing-table syntax 1:1.

use std::collections::BTreeMap;

use async_nats::HeaderMap;
use async_trait::async_trait;
use bytes::Bytes;
use futures::StreamExt;

use crate::router::BackendId;

use super::{Backend, BackendError, Envelope, EnvelopeStream};

/// Production NATS backend.
#[derive(Clone)]
pub struct NatsBackend {
    client: async_nats::Client,
}

impl NatsBackend {
    /// Connect to the NATS server at `url`.
    pub async fn connect(url: &str) -> Result<Self, BackendError> {
        let client = async_nats::connect(url)
            .await
            .map_err(|e| BackendError::Unavailable {
                backend: BackendId::Nats,
                message: format!("could not connect to NATS at {url}: {e}"),
            })?;
        Ok(Self { client })
    }

    /// Wrap an already-connected NATS client (used by tests).
    pub fn from_client(client: async_nats::Client) -> Self {
        Self { client }
    }
}

#[async_trait]
impl Backend for NatsBackend {
    fn id(&self) -> BackendId {
        BackendId::Nats
    }

    async fn publish(&self, envelope: Envelope) -> Result<(), BackendError> {
        let mut headers = HeaderMap::new();
        for (k, v) in &envelope.headers {
            headers.insert(k.as_str(), v.as_str());
        }
        if let Some(schema) = envelope.schema_id.as_deref() {
            headers.insert("x-of-schema-id", schema);
        }
        self.client
            .publish_with_headers(envelope.topic.clone(), headers, envelope.payload)
            .await
            .map_err(|e| BackendError::Transport {
                backend: BackendId::Nats,
                message: e.to_string(),
            })?;
        // `flush` is intentionally not awaited per-call: the core NATS client
        // already buffers small writes efficiently and waiting here would
        // serialise publishes under load.
        Ok(())
    }

    async fn subscribe(&self, pattern: &str) -> Result<EnvelopeStream, BackendError> {
        let sub = self
            .client
            .subscribe(pattern.to_string())
            .await
            .map_err(|e| BackendError::Transport {
                backend: BackendId::Nats,
                message: e.to_string(),
            })?;

        let stream = sub.map(|msg| {
            let mut headers = BTreeMap::new();
            let mut schema_id = None;
            if let Some(h) = msg.headers {
                for (name, values) in h.iter() {
                    if let Some(value) = values.iter().next() {
                        let key = name.to_string();
                        let val = value.to_string();
                        if key == "x-of-schema-id" {
                            schema_id = Some(val);
                        } else {
                            headers.insert(key, val);
                        }
                    }
                }
            }
            Ok(Envelope {
                topic: msg.subject.to_string(),
                payload: Bytes::from(msg.payload.to_vec()),
                headers,
                schema_id,
            })
        });

        Ok(Box::pin(stream))
    }
}
