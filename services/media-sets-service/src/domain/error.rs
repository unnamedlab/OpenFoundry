//! Domain-level error type. Crosses the REST/gRPC boundary via the
//! `From` impls in `crate::handlers` and `crate::grpc`.

use thiserror::Error;

#[derive(Debug, Error)]
pub enum MediaError {
    #[error("media set `{0}` not found")]
    MediaSetNotFound(String),
    #[error("media item `{0}` not found")]
    MediaItemNotFound(String),
    #[error("transaction `{0}` not found")]
    TransactionNotFound(String),
    #[error("media set `{0}` is transactionless; transactions are not allowed")]
    Transactionless(String),
    #[error("transaction `{0}` is already in terminal state `{1}`")]
    TransactionTerminal(String, String),
    #[error("invalid request: {0}")]
    BadRequest(String),
    /// Cedar engine denied the request. The string carries a
    /// human-readable reason — typically "missing clearance: SECRET".
    /// Maps to HTTP 403.
    #[error("forbidden: {0}")]
    Forbidden(String),
    /// Cedar engine raised an internal error (entity hydration,
    /// missing engine, signing failure). Maps to HTTP 500 — these are
    /// our bugs, not the operator's.
    #[error("authz internal error: {0}")]
    Authz(String),
    #[error("storage error: {0}")]
    Storage(String),
    /// A neighbour service required by this request (today: only
    /// `connector-management-service` for virtual-set URL resolution)
    /// is unconfigured or unreachable. Surfaces as HTTP 503 so callers
    /// can retry once the dependency is back, rather than treating it
    /// as a permanent 4xx.
    #[error("upstream service unavailable: {0}")]
    UpstreamUnavailable(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    /// Outbox enqueue failed (Postgres write or payload serialization).
    /// We deliberately map this to a generic 5xx — the caller cannot
    /// recover and dropping the event would violate ADR-0022's
    /// at-least-once guarantee.
    #[error("audit outbox error: {0}")]
    Outbox(String),
}

impl From<outbox::OutboxError> for MediaError {
    fn from(value: outbox::OutboxError) -> Self {
        match value {
            outbox::OutboxError::Db(err) => Self::Database(err),
            outbox::OutboxError::Serialize(err) => Self::Outbox(err.to_string()),
        }
    }
}

pub type MediaResult<T> = Result<T, MediaError>;
