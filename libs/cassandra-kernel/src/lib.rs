//! Cassandra (CQL) client kernel shared by OpenFoundry services.
//!
//! Built on top of the official ScyllaDB Rust driver
//! ([`scylla`](https://docs.rs/scylla)). The Scylla driver speaks
//! native CQL v4/v5 against any Cassandra cluster, supports
//! token-aware routing, prepared-statement caching, paged streaming
//! and DC-local load balancing — which is why ADR-0020 picks it as
//! the only Rust driver allowed in the codebase.
//!
//! This crate centralises the four things every OpenFoundry service
//! that talks to Cassandra needs:
//!
//! * a [`SessionBuilder`] with the standard contact points, the local
//!   datacenter, retry / speculative-execution policies and request
//!   tracing hook (see [`session`]);
//! * a process-wide [`shared`] holder so services obtain `Arc<Session>`
//!   exactly once per process;
//! * a small set of [`query`] helpers that codify the modelling rules
//!   from ADR-0020 (paged queries, restricted LWT, restricted LOGGED
//!   batches, prepared-statement cache);
//! * a [`migrate`] runtime plus the [`cql_migrate!`] macro for
//!   versioned, idempotent CQL migrations (Cassandra has no native
//!   reversible migrations; rollbacks are forward-only).
//!
//! See `README.md` for a full API tour and usage recipes.

#![forbid(unsafe_code)]
#![warn(missing_docs)]

pub mod error;
pub mod migrate;
pub mod query;
#[cfg(feature = "repos")]
pub mod repos;
pub mod session;
pub mod shared;

pub use error::{KernelError, KernelResult};
pub use migrate::{Migration, MigrationOutcome};
pub use query::{PreparedCache, batch_logged, lwt_insert_if_not_exists, paged_query};
pub use session::{ClusterConfig, SessionBuilder};
pub use shared::SharedSession;

// Re-export the underlying driver so callers do not have to add a
// direct `scylla` dependency for the few types they need at the
// boundary (Session, PreparedStatement, QueryResult, Consistency…).
pub use scylla;
