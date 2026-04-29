//! `pgvector` backend skeleton.
//!
//! This module exists to anchor the contract between
//! [`crate::backend::VectorBackend`] and the historical pgvector
//! implementation. The full PostgreSQL/pgvector wiring (sqlx pool, schema
//! migrations, `<->` operator, ivfflat / HNSW indexes) is tracked
//! separately; here we provide the type and a `VectorBackend` impl that
//! returns [`BackendError::Unimplemented`] so:
//!
//! * the trait is statically known to be satisfiable by pgvector, and
//! * downstream consumers can already program against
//!   `Box<dyn VectorBackend>` without picking a concrete backend yet.
//!
//! The public surface (the `PgVectorBackend` struct and its trait impl)
//! is intentionally minimal so adding the real implementation later is a
//! non-breaking change.

use std::collections::BTreeMap;

use async_trait::async_trait;

use crate::backend::{BackendError, BackendResult, Filter, QueryHit, VectorBackend};

/// Handle to a pgvector-backed collection.
///
/// Construction parameters (connection pool, table name, embedding
/// dimension, …) will be added when the implementation lands. For now
/// the type is a unit struct so the trait impl below compiles.
#[derive(Debug, Default, Clone)]
pub struct PgVectorBackend {
    _private: (),
}

impl PgVectorBackend {
    /// Create a new (currently inert) pgvector backend handle.
    pub fn new() -> Self {
        Self { _private: () }
    }
}

#[async_trait]
impl VectorBackend for PgVectorBackend {
    async fn upsert(
        &self,
        _doc_id: &str,
        _fields: &BTreeMap<String, serde_json::Value>,
        _embedding: &[f32],
    ) -> BackendResult<()> {
        Err(BackendError::Unimplemented("pgvector::upsert"))
    }

    async fn delete(&self, _doc_id: &str) -> BackendResult<()> {
        Err(BackendError::Unimplemented("pgvector::delete"))
    }

    async fn hybrid_query(
        &self,
        _text: &str,
        _embedding: &[f32],
        _filter: &Filter,
        _top_k: usize,
    ) -> BackendResult<Vec<QueryHit>> {
        Err(BackendError::Unimplemented("pgvector::hybrid_query"))
    }
}
