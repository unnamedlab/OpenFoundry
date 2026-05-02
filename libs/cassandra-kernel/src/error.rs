//! Error type wrapping driver and kernel-level failures.

use thiserror::Error;

/// Result alias used throughout the kernel.
pub type KernelResult<T> = Result<T, KernelError>;

/// All errors surfaced by the kernel.
///
/// Driver errors are wrapped opaquely so callers don't need a direct
/// `scylla` dependency just to match on them. The variants that carry
/// extra context (modelling violations, migration drift) are first-
/// class because services should react to them differently than to a
/// transient driver error.
#[derive(Debug, Error)]
pub enum KernelError {
    /// Failed to build the underlying [`scylla::Session`].
    #[error("failed to build Cassandra session: {0}")]
    SessionBuild(#[from] scylla::transport::errors::NewSessionError),

    /// CQL execution failure (timeout, unavailable, syntax, …).
    #[error("CQL query failed: {0}")]
    Query(#[from] scylla::transport::errors::QueryError),

    /// Modelling-rule violation detected at runtime by a kernel
    /// helper (e.g. a LOGGED batch spanning multiple partitions).
    /// See ADR-0020 for the full list of forbidden patterns.
    #[error("modelling rule violated: {0}")]
    ModellingRule(String),

    /// A versioned migration's recorded checksum does not match the
    /// statement currently in source. Migrations are immutable; if a
    /// statement must change, a new version must be added.
    #[error(
        "migration {version} ({name}) drift detected: stored checksum {stored} != current {current}"
    )]
    MigrationDrift {
        /// Version number of the migration.
        version: i32,
        /// Human-readable migration name.
        name: String,
        /// Checksum currently stored in the cluster.
        stored: String,
        /// Checksum computed from the source.
        current: String,
    },

    /// Catch-all for unexpected internal state.
    #[error(transparent)]
    Other(#[from] anyhow::Error),
}
