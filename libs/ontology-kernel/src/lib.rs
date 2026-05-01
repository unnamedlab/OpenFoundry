//! Shared ontology kernel: configuration, domain logic, models and HTTP
//! handlers reused by every `ontology-*` and `object-database-service` crate.
//!
//! Historically this tree was injected into each service via
//! `#[path = "../../../../libs/ontology-kernel/src/.../mod.rs"]`. It is now a
//! real Cargo crate so it can be linted, tested and consumed via
//! `use ontology_kernel::handlers::actions::*;` etc.

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;
