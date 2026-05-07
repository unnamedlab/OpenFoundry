//! Global-branching domain: SQL-backed CRUD + Kafka subscriber port.
//!
//! Absorbed from `global-branch-service` per ADR-0030 (S8 merge).

pub mod handlers;
pub mod model;
pub mod store;
pub mod subscriber;
