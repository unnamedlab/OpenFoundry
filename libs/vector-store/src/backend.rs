//! Backend-agnostic abstraction over vector / hybrid search engines.
//!
//! `vector-store` historically grew around `pgvector`. To allow alternative
//! engines (notably Vespa for hybrid BM25 + ANN retrieval) without leaking
//! backend specifics into callers, every concrete backend implements the
//! [`VectorBackend`] trait defined here.
//!
//! The trait is intentionally minimal: `upsert`, `delete` and a single
//! [`hybrid_query`](VectorBackend::hybrid_query) entry point that combines
//! lexical (text) and dense-vector (embedding) signals. Backends that only
//! support one of the two signals can ignore the other.
//!
//! Adding a new backend is purely additive and does not affect existing
//! consumers of any other backend.

use std::collections::BTreeMap;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

/// Logical document fed into a backend.
///
/// Fields are arbitrary JSON values so the same trait can serve simple
/// `text + tenant_id` retrieval as well as richer catalog records. Every
/// document also carries a dense `embedding` (may be empty for backends
/// that don't index vectors).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Document {
    /// Stable identifier of the document inside its logical collection.
    pub id: String,
    /// Backend-agnostic field bag. Concrete backends decide how to map
    /// each entry onto their own schema (column, attribute, JSON field…).
    pub fields: BTreeMap<String, serde_json::Value>,
    /// Dense embedding. Length must match the embedding dimension the
    /// backend was configured for; empty means "no vector signal".
    pub embedding: Vec<f32>,
}

/// Restrictive filter applied on top of the hybrid query.
///
/// Kept deliberately small: backends translate this into their native
/// filtering language (SQL `WHERE`, YQL clauses…). Exact-match equality
/// is supported on every backend; ranges/sets can be added later in a
/// backwards-compatible way.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Filter {
    /// AND-combined exact-match constraints, e.g. `{ "tenant_id": "acme" }`.
    pub equals: BTreeMap<String, serde_json::Value>,
}

impl Filter {
    /// Convenience constructor for a single equality clause.
    pub fn eq(field: impl Into<String>, value: impl Into<serde_json::Value>) -> Self {
        let mut equals = BTreeMap::new();
        equals.insert(field.into(), value.into());
        Self { equals }
    }
}

/// One result row returned by [`VectorBackend::hybrid_query`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueryHit {
    /// Document id.
    pub id: String,
    /// Backend-assigned relevance score; semantics are backend-specific
    /// but ordering is always "higher is more relevant".
    pub score: f64,
    /// Optional summary fields returned by the backend.
    #[serde(default)]
    pub fields: BTreeMap<String, serde_json::Value>,
}

/// Errors returned by every backend.
#[derive(Debug, thiserror::Error)]
pub enum BackendError {
    /// Network / transport failure talking to the backend.
    #[error("transport error: {0}")]
    Transport(String),
    /// Backend rejected the request (4xx/5xx, SQL error, …).
    #[error("backend error: {0}")]
    Backend(String),
    /// Failed to (de)serialize the wire payload.
    #[error("serialization error: {0}")]
    Serialization(String),
    /// The backend doesn't (yet) support the requested operation.
    #[error("operation not supported by backend: {0}")]
    Unsupported(&'static str),
    /// Operation is defined in the trait but the implementation is still
    /// pending. Used by skeleton backends that haven't been wired yet.
    #[error("operation not implemented for this backend: {0}")]
    Unimplemented(&'static str),
}

/// Convenience result alias.
pub type BackendResult<T> = Result<T, BackendError>;

/// Common contract every search/vector backend implements.
///
/// All methods are async and `Send + Sync` so backends can be shared
/// across tasks behind an `Arc`.
#[async_trait]
pub trait VectorBackend: Send + Sync {
    /// Insert or replace a document. Implementations must be idempotent
    /// on `doc_id`.
    async fn upsert(
        &self,
        doc_id: &str,
        fields: &BTreeMap<String, serde_json::Value>,
        embedding: &[f32],
    ) -> BackendResult<()>;

    /// Remove a document by id. Deleting a non-existent id must succeed
    /// (no-op) so callers can run idempotent reconciliation loops.
    async fn delete(&self, doc_id: &str) -> BackendResult<()>;

    /// Run a hybrid (lexical + dense) query.
    ///
    /// * `text` – BM25 query string. Empty means "match all" (filter only).
    /// * `embedding` – dense vector for ANN search. Empty means "skip ANN".
    /// * `filter` – AND-combined restrictions.
    /// * `top_k` – maximum number of hits to return.
    async fn hybrid_query(
        &self,
        text: &str,
        embedding: &[f32],
        filter: &Filter,
        top_k: usize,
    ) -> BackendResult<Vec<QueryHit>>;
}
