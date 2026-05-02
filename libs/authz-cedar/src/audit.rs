//! Audit sink for authorization decisions.
//!
//! Per ADR-0027 (and the Foundry-parity audit requirements) every
//! authorization decision must be observable: who asked for what,
//! against which resource, and what the engine answered. The
//! emission path is deliberately **fire-and-forget**: a slow audit
//! sink must NEVER stall the request hot path.
//!
//! Implementations:
//!
//! * [`NoopAuditSink`] — drops everything. Default for tests.
//! * [`TracingAuditSink`] — logs at `info!` level. Useful in dev.
//! * Production code wires a Kafka producer behind this trait that
//!   publishes to topic `audit.authz.v1`. The crate intentionally
//!   does not pull rdkafka itself — the service that owns Kafka
//!   wiring (e.g. `authorization-policy-service`) provides the impl.

use std::sync::Arc;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// Wire format for the `audit.authz.v1` Kafka topic.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthzAuditEvent {
    pub timestamp: DateTime<Utc>,
    pub principal: String,
    pub action: String,
    pub resource: String,
    pub decision: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tenant: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub policy_ids: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub diagnostics: Vec<String>,
}

#[async_trait]
pub trait AuthzAuditSink: Send + Sync + 'static {
    async fn emit(&self, event: AuthzAuditEvent);
}

/// Drops every event. Default for tests and the in-memory engine.
#[derive(Debug, Default, Clone, Copy)]
pub struct NoopAuditSink;

#[async_trait]
impl AuthzAuditSink for NoopAuditSink {
    async fn emit(&self, _event: AuthzAuditEvent) {}
}

/// Logs every decision at `info!` level. Useful for dev / smoke runs.
#[derive(Debug, Default, Clone, Copy)]
pub struct TracingAuditSink;

#[async_trait]
impl AuthzAuditSink for TracingAuditSink {
    async fn emit(&self, event: AuthzAuditEvent) {
        tracing::info!(
            target: "authz.audit",
            principal = %event.principal,
            action = %event.action,
            resource = %event.resource,
            decision = %event.decision,
            tenant = ?event.tenant,
            policies = ?event.policy_ids,
            "authz decision"
        );
    }
}

/// Type-erased handle. The engine owns one of these.
pub type AuditSinkHandle = Arc<dyn AuthzAuditSink>;
