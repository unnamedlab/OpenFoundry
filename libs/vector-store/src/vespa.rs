//! Vespa backend for hybrid (BM25 + ANN HNSW + phased ranking) search.
//!
//! This module is gated behind the `vespa` Cargo feature. Enabling it is
//! purely additive: the [`pgvector`](crate::pgvector) module and the
//! [`VectorBackend`](crate::backend::VectorBackend) trait surface remain
//! unchanged.
//!
//! # Why a hand-rolled HTTP client?
//!
//! As of writing there is no widely-adopted, maintained Vespa client crate
//! on crates.io that supports both the Document v1 CRUD API and the Search
//! API with custom YQL + tensor query parameters. Vespa's public APIs are
//! plain HTTP/JSON, so we wrap them with `reqwest` (already a workspace
//! dependency) and `serde_json`. The advisory database has been checked
//! for both crates at the versions used by the workspace and reports no
//! known vulnerabilities.
//!
//! # Endpoints used
//!
//! * `POST /document/v1/<namespace>/<doctype>/docid/<id>` – upsert
//! * `DELETE /document/v1/<namespace>/<doctype>/docid/<id>` – delete
//! * `POST /search/` – YQL query with `input.query(q_embedding)` tensor
//!
//! See the [Vespa HTTP API
//! reference](https://docs.vespa.ai/en/reference/document-v1-api-reference.html)
//! and the [Search API
//! reference](https://docs.vespa.ai/en/reference/query-api-reference.html).
//!
//! # Recommended schema (deploy this in your application package)
//!
//! ```text
//! schema doc {
//!   document doc {
//!     field text type string {
//!       indexing: index | summary
//!       index: enable-bm25
//!     }
//!     field tenant_id type string {
//!       indexing: attribute | summary
//!     }
//!     field embedding type tensor<float>(x[N]) {
//!       indexing: attribute | index
//!       attribute { distance-metric: angular }
//!       index {
//!         hnsw {
//!           max-links-per-node: 16
//!           neighbors-to-explore-at-insert: 200
//!         }
//!       }
//!     }
//!   }
//!
//!   rank-profile hybrid {
//!     inputs { query(q_embedding) tensor<float>(x[N]) }
//!     first-phase  { expression: bm25(text) + closeness(field, embedding) }
//!     second-phase { expression: firstPhase }      // tune as needed
//!   }
//! }
//! ```
//!
//! See [`VespaBackend`] for usage.

mod client;

pub use client::{VespaBackend, VespaConfig};

#[cfg(test)]
mod tests;
