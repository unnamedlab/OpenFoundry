//! Search backend abstraction for OpenFoundry.
//!
//! Re-exports the canonical [`SearchBackend`] trait from
//! `storage-abstraction` (so services depend on this crate, not on
//! the storage one, when they only need search) and ships two HTTP
//! client implementations behind cargo features:
//!
//! * `vespa` — production target, per
//!   [ADR-0028](../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md);
//! * `opensearch` — dev and CI fallback so contributors do not have
//!   to run a full Vespa cluster on their laptop.
//!
//! Both backends speak the same trait surface defined in
//! `storage_abstraction::repositories`. The contract suite under
//! [`contract`] validates that **any** implementation honours the
//! semantics services rely on (deny-stale-writes, tenant isolation,
//! bulk error reporting, vector top-k, …); it is parameterised over
//! the backend so it runs identically against the in-memory fake,
//! Vespa, and OpenSearch.
//!
//! Note on dependencies: we deliberately depend on `reqwest` rather
//! than the official `opensearch = "2"` client crate. The trait
//! surface we exercise is small (six methods) and the JSON wire
//! format is stable; pulling in the official crate would drag in
//! `aws-sigv4`, `hyper-tls`, and a parallel TLS stack we already
//! have via `reqwest`'s `rustls` feature. Documented divergence
//! from S0.8.c.

#![forbid(unsafe_code)]
#![warn(missing_docs)]

pub use storage_abstraction::repositories::{
    BulkOutcome, IndexDoc, ObjectId, Page, PagedResult, ReadConsistency, RepoError, RepoResult,
    SearchBackend, SearchHit, SearchQuery, TenantId, TypeId, VectorQuery,
};

#[cfg(feature = "vespa")]
pub mod vespa;

#[cfg(feature = "opensearch")]
pub mod opensearch;

#[cfg(feature = "vespa")]
pub use vespa::VespaSearchBackend;

#[cfg(feature = "opensearch")]
pub use opensearch::OpenSearchBackend;

pub mod contract;

/// Sanitize an arbitrary string into a Vespa / OpenSearch-friendly
/// identifier (lowercase, `[a-z0-9_]` only). Used by both backends
/// when computing document type names and index names.
pub fn sanitize_doc_type(s: &str) -> String {
    s.to_ascii_lowercase()
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '_' {
                c
            } else {
                '_'
            }
        })
        .collect()
}

/// Backend selection knob.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BackendChoice {
    /// Production target.
    Vespa,
    /// Dev / CI fallback.
    OpenSearch,
}

impl BackendChoice {
    /// Parse `SEARCH_BACKEND=vespa|opensearch` (case-insensitive).
    /// Returns `None` for unset or unrecognised values; callers
    /// decide the default for their environment.
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "vespa" => Some(Self::Vespa),
            "opensearch" | "os" => Some(Self::OpenSearch),
            _ => None,
        }
    }

    /// Resolve from the `SEARCH_BACKEND` env var. Defaults to
    /// `Vespa` (production-safe default; dev/CI must opt out).
    pub fn from_env() -> Self {
        std::env::var("SEARCH_BACKEND")
            .ok()
            .as_deref()
            .and_then(Self::parse)
            .unwrap_or(Self::Vespa)
    }
}

/// Build a [`SearchBackend`] from the `SEARCH_BACKEND` environment
/// variable plus a `SEARCH_ENDPOINT` URL.
///
/// Returns `Err` if the chosen backend's feature flag is not enabled
/// at build time, or if `SEARCH_ENDPOINT` is unset.
#[cfg(any(feature = "vespa", feature = "opensearch"))]
pub fn search_backend_from_env() -> Result<std::sync::Arc<dyn SearchBackend>, RepoError> {
    let endpoint = std::env::var("SEARCH_ENDPOINT")
        .map_err(|_| RepoError::InvalidArgument("SEARCH_ENDPOINT not set".into()))?;
    match BackendChoice::from_env() {
        #[cfg(feature = "vespa")]
        BackendChoice::Vespa => Ok(std::sync::Arc::new(VespaSearchBackend::new(endpoint))),
        #[cfg(not(feature = "vespa"))]
        BackendChoice::Vespa => Err(RepoError::InvalidArgument(
            "search-abstraction was built without the `vespa` feature".into(),
        )),
        #[cfg(feature = "opensearch")]
        BackendChoice::OpenSearch => Ok(std::sync::Arc::new(OpenSearchBackend::new(endpoint))),
        #[cfg(not(feature = "opensearch"))]
        BackendChoice::OpenSearch => Err(RepoError::InvalidArgument(
            "search-abstraction was built without the `opensearch` feature".into(),
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn backend_choice_parses() {
        assert_eq!(BackendChoice::parse("vespa"), Some(BackendChoice::Vespa));
        assert_eq!(
            BackendChoice::parse("OpenSearch"),
            Some(BackendChoice::OpenSearch)
        );
        assert_eq!(BackendChoice::parse("os"), Some(BackendChoice::OpenSearch));
        assert_eq!(BackendChoice::parse("redis"), None);
    }

    #[test]
    fn sanitize_doc_type_lowercases_and_strips() {
        assert_eq!(sanitize_doc_type("Foo-Bar.Baz"), "foo_bar_baz");
        assert_eq!(sanitize_doc_type("ALREADY_OK"), "already_ok");
    }
}
