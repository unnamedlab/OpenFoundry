//! Embedding generation and vector / hybrid search.
//!
//! This crate exposes a small backend-agnostic abstraction
//! ([`VectorBackend`](backend::VectorBackend)) and one or more concrete
//! implementations gated behind Cargo features:
//!
//! * `pgvector` (default) – PostgreSQL + `pgvector` extension. Currently a
//!   skeleton; see [`pgvector`].
//! * `vespa` – hybrid search (BM25 + ANN HNSW + phased ranking) backed by
//!   [Vespa](https://vespa.ai) over its HTTP/JSON APIs (`/document/v1/...`
//!   and `/search/`). See [`vespa`].
//!
//! The backend modules are independent: enabling one never forces a
//! dependency on the others. Adding `vespa` is purely additive and does
//! not change the existing `pgvector` surface.

pub mod backend;

pub mod pgvector;

#[cfg(feature = "vespa")]
pub mod vespa;

pub use backend::{BackendError, BackendResult, Document, Filter, QueryHit, VectorBackend};
