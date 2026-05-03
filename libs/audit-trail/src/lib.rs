//! Shared audit-trail building blocks for OpenFoundry services.
//!
//! The crate exposes two surfaces:
//!
//! 1. [`middleware`] — an Axum/Tower layer that emits a structured
//!    `tracing::info!(target = "audit", ...)` per HTTP request. The
//!    `audit-compliance-service` collector picks those up.
//! 2. [`events`] — the canonical audit event vocabulary plus the
//!    Postgres-outbox publisher (`outbox` feature). Services emit
//!    [`events::AuditEvent`] inside the same SQL transaction as the
//!    primary write; Debezium drains the outbox to the
//!    [`events::TOPIC`] (`audit.events.v1`) Kafka topic that
//!    `audit-sink` consumes (ADR-0022).

pub mod events;
pub mod middleware;

pub use middleware::{AuditLayer, AuditService, audit_layer};
