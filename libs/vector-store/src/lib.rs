pub mod backend;
pub mod pgvector;
pub mod vespa;

pub use backend::{
    BackendConfig, BackendKind, Cursor, EmbeddingRecord, Hit, HybridQuery, VectorBackend,
    VectorBackendError, VectorBackendRouter, build_backend,
};