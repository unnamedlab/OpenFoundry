//! Shared test utilities for OpenFoundry services.
//!
//! Provides three families of helpers used by the integration test
//! suites under `services/*/tests/` and `libs/*/tests/`:
//!
//! * [`containers`] — boot ephemeral infrastructure (Postgres) via
//!   `testcontainers`. Migration application is left to the caller via
//!   `sqlx::migrate!()` so each crate keeps its own migration root.
//! * [`fixtures`] — deterministic JWT issuance and SQL seed helpers
//!   (datasets, branches, transactions, markings).
//! * [`mocks`] — `wiremock` server builders for stubbing neighbour
//!   services (lineage, retention, audit, catalog).
//!
//! Two more harnesses live behind cargo features so they only enter
//! the compile graph for crates that actually need them:
//!
//! * `cassandra` (feature `it-cassandra`) — boots a single-node
//!   `cassandra:5.0` container and returns a connected
//!   `scylla::Session`.
//! * `temporal` (feature `it-temporal`) — boots
//!   `temporalio/auto-setup:1.24` and exposes the gRPC frontend.
//!
//! All helpers are thin wrappers over the upstream crates and are
//! intentionally permissive (they panic on misuse rather than returning
//! `Result`) — they are only meant for tests.

pub mod containers;
pub mod fixtures;
pub mod mocks;

#[cfg(feature = "it-cassandra")]
pub mod cassandra;

#[cfg(feature = "it-temporal")]
pub mod temporal;
