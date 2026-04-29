use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
/// A single embedding record stored in the vector backend.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EmbeddingRecord {
    pub tenant_id: String,
    pub namespace: String,
    pub doc_id: String,
    pub vector: Vec<f32>,
    pub payload: Value,
    pub ts: DateTime<Utc>,
}

/// A hybrid query combining dense vector search with keyword/BM25 filtering.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HybridQuery {
    pub tenant_id: String,
    pub namespace: String,
    pub vector: Vec<f32>,
    /// Optional keyword text for BM25/lexical matching.
    pub keyword: Option<String>,
    /// Optional metadata filter (JSON predicate).
    pub filter: Option<Value>,
    pub top_k: usize,
    /// Minimum relevance score threshold (0.0–1.0).
    pub min_score: f32,
}

/// A single search result hit.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Hit {
    pub doc_id: String,
    pub score: f32,
    pub payload: Value,
}

/// Opaque cursor for paginated iteration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Cursor(pub String);

/// Errors returned by vector backend operations.
#[derive(Debug, thiserror::Error)]
pub enum VectorBackendError {
    #[error("backend not available: {0}")]
    Unavailable(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("HTTP error: {0}")]
    Http(#[from] reqwest::Error),
    #[error("serialization error: {0}")]
    Serialization(#[from] serde_json::Error),
    #[error("invalid configuration: {0}")]
    Config(String),
}

/// Discriminant for choosing which backend to instantiate.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BackendKind {
    Pgvector,
    Vespa,
}

/// Configuration for constructing a VectorBackend.
#[derive(Debug, Clone, Deserialize)]
pub struct BackendConfig {
    pub kind: BackendKind,
    /// PostgreSQL connection URL (required for pgvector).
    pub database_url: Option<String>,
    /// Vespa base URL e.g. "http://localhost:8080" (required for vespa).
    pub vespa_url: Option<String>,
    /// Embedding dimension (required for vespa schema).
    #[serde(default = "default_dim")]
    pub dim: usize,
}

fn default_dim() -> usize {
    768
}

/// The core vector backend trait.
#[async_trait]
pub trait VectorBackend: Send + Sync {
    /// Upsert an embedding record, keyed by (tenant_id, namespace, doc_id).
    async fn upsert(&self, record: EmbeddingRecord) -> Result<(), VectorBackendError>;

    /// Delete a record by (tenant_id, namespace, doc_id).
    async fn delete(
        &self,
        tenant_id: &str,
        namespace: &str,
        doc_id: &str,
    ) -> Result<(), VectorBackendError>;

    /// Execute a hybrid vector+keyword query.
    async fn hybrid_query(&self, query: HybridQuery) -> Result<Vec<Hit>, VectorBackendError>;

    /// Return the health status of the backend.
    async fn health(&self) -> Result<(), VectorBackendError>;

    /// Paginated iteration over all embeddings for a tenant/namespace.
    /// Returns (records, next_cursor). If next_cursor is None, iteration is complete.
    async fn iter_embeddings(
        &self,
        tenant_id: &str,
        namespace: &str,
        cursor: Option<Cursor>,
        batch_size: usize,
    ) -> Result<(Vec<EmbeddingRecord>, Option<Cursor>), VectorBackendError>;

    /// Build a backend from a BackendConfig.
    async fn from_config(
        config: &BackendConfig,
    ) -> Result<Box<dyn VectorBackend>, VectorBackendError>
    where
        Self: Sized;
}

/// Factory function: build a VectorBackend from config.
pub async fn build_backend(
    config: &BackendConfig,
) -> Result<Box<dyn VectorBackend>, VectorBackendError> {
    match config.kind {
        BackendKind::Pgvector => {
            let b = crate::pgvector::PgvectorBackend::new(config).await?;
            Ok(Box::new(b))
        }
        BackendKind::Vespa => {
            let b = crate::vespa::VespaBackend::new(config)?;
            Ok(Box::new(b))
        }
    }
}
