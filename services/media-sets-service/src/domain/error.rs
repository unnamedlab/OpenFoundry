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
    #[error("branch `{0}` not found")]
    BranchNotFound(String),
    #[error("media set `{0}` is transactionless; transactions are not allowed")]
    Transactionless(String),
    /// Per `Advanced media set settings.md`: "Transactionless media set
    /// branches cannot be reset to an empty view." Mapped to 422.
    #[error("media set `{0}` is transactionless; branch reset is not allowed")]
    TransactionlessRejectsReset(String),
    /// Per `Incremental media sets.md`: "Transactionless media sets
    /// use the `modify` write mode and cannot use the `replace` write
    /// mode." Mapped to 422.
    #[error("media set `{0}` is transactionless; REPLACE write mode is not allowed")]
    TransactionlessRejectsReplace(String),
    /// Per `Advanced media set settings.md`: "A maximum of 10,000
    /// items can be written in a single transaction." Mapped to 422
    /// with code `MEDIA_SET_TRANSACTION_TOO_LARGE`.
    #[error("transaction `{0}` already holds the maximum {1} items")]
    TransactionTooLarge(String, i64),
    /// Returned by `merge_branch_op` when `FAIL_ON_CONFLICT` was
    /// requested and at least one path is live on both branches.
    /// Mapped to HTTP 409 with the conflict surface in the body.
    #[error("merge conflict on {} paths", .0.len())]
    MergeConflict(Vec<String>),
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
