//! Concrete messaging backends behind the [`Backend`] trait.

mod kafka;
mod nats;
mod registry;

pub use kafka::KafkaUnavailableBackend;
pub use nats::NatsBackend;
pub use registry::BackendRegistry;

use std::collections::BTreeMap;
use std::pin::Pin;

use async_trait::async_trait;
use bytes::Bytes;
use futures::Stream;
use thiserror::Error;

use crate::router::BackendId;

/// One unit of work flowing through the router.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Envelope {
    pub topic: String,
    pub payload: Bytes,
    pub headers: BTreeMap<String, String>,
    pub schema_id: Option<String>,
}

/// Errors a backend can surface to the router.
#[derive(Debug, Error)]
pub enum BackendError {
    /// The backend is configured but its underlying transport is not
    /// reachable (e.g. broker down, not yet implemented).
    #[error("backend `{backend}` is unavailable: {message}")]
    Unavailable { backend: BackendId, message: String },
    /// Generic transport error returned by the backend's client library.
    #[error("backend `{backend}` transport error: {message}")]
    Transport { backend: BackendId, message: String },
    /// The backend rejected the call with an explicit, mappable error.
    #[error("backend `{backend}` rejected the request: {message}")]
    Rejected { backend: BackendId, message: String },
}

impl BackendError {
    pub fn backend(&self) -> BackendId {
        match self {
            BackendError::Unavailable { backend, .. }
            | BackendError::Transport { backend, .. }
            | BackendError::Rejected { backend, .. } => *backend,
        }
    }
}

/// Stream of inbound envelopes for a subscription.
pub type EnvelopeStream =
    Pin<Box<dyn Stream<Item = Result<Envelope, BackendError>> + Send + 'static>>;

/// Behaviour every messaging backend exposes to the router.
#[async_trait]
pub trait Backend: Send + Sync {
    fn id(&self) -> BackendId;

    /// Publish a single envelope. Returning `Ok(())` does not imply broker
    /// acknowledgement semantics — those are backend-specific.
    async fn publish(&self, envelope: Envelope) -> Result<(), BackendError>;

    /// Open a subscription for a backend-specific topic pattern. The pattern
    /// is the same string the user gave to `Subscribe`; backends may translate
    /// it to their native syntax.
    async fn subscribe(&self, pattern: &str) -> Result<EnvelopeStream, BackendError>;
}
