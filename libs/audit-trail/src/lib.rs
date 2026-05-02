//! Shared audit-trail building blocks for OpenFoundry services.
//!
//! Today this crate only exposes the lightweight [`middleware`] used by
//! Axum-based services to emit one tracing span per HTTP request that the
//! `audit-compliance-service` collector picks up. The richer event emitter
//! and request-context modules are reserved for a follow-up change.

pub mod middleware;

pub use middleware::{AuditLayer, AuditService, audit_layer};
