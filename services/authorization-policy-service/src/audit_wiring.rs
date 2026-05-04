//! Production wiring for the authorization audit pipeline (S0.7.g).
//!
//! Builds an [`AuditSinkHandle`] backed by [`KafkaAuthzAuditSink`] using
//! the standard OpenFoundry Kafka environment contract (mirrors
//! `data_bus_config_from_env` in the `ontology-indexer` /
//! `lineage-service` / `ai-sink` runtimes). The returned handle is
//! intended to be installed on the [`AuthzEngine`] at service startup
//! so every authorization decision lands on `audit.authz.v1`.
//!
//! Recognised env vars:
//!
//! * `KAFKA_BOOTSTRAP_SERVERS` (required) — comma-separated `host:port`.
//! * `KAFKA_SASL_USERNAME` / `KAFKA_CLIENT_ID` — service identity.
//!   Falls back to `"authorization-policy-service"`.
//! * `KAFKA_SASL_PASSWORD` — when set, switches to SCRAM-SHA-512 over
//!   SASL_SSL; when unset, runs against an unauthenticated broker
//!   (`PLAINTEXT`), matching the dev-cluster default.
//! * `KAFKA_SASL_MECHANISM` / `KAFKA_SECURITY_PROTOCOL` — explicit
//!   overrides for the SASL/security knobs.

use std::sync::Arc;

use authz_cedar::{AuditSinkHandle, KAFKA_AUDIT_TOPIC, KafkaAuthzAuditSink};
use event_bus_data::{DataBusConfig, DataPublisher, KafkaPublisher, ServicePrincipal};
use thiserror::Error;

const SERVICE_NAME: &str = "authorization-policy-service";

#[derive(Debug, Error)]
pub enum AuditWiringError {
    #[error("required environment variable {0} is not set")]
    MissingEnv(&'static str),
    #[error("kafka publisher build failed: {0}")]
    Publisher(#[from] event_bus_data::PublishError),
}

/// Build the production Kafka audit sink from the environment.
///
/// Returns an `AuditSinkHandle` ready to plug into
/// `AuthzEngine::new(store, sink)`.
pub fn kafka_audit_sink_from_env() -> Result<AuditSinkHandle, AuditWiringError> {
    let publisher = build_publisher_from_env()?;
    let sink = KafkaAuthzAuditSink::new(
        Arc::new(publisher) as Arc<dyn DataPublisher>,
        KAFKA_AUDIT_TOPIC.to_string(),
    );
    Ok(Arc::new(sink))
}

fn build_publisher_from_env() -> Result<KafkaPublisher, AuditWiringError> {
    let brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS")
        .map_err(|_| AuditWiringError::MissingEnv("KAFKA_BOOTSTRAP_SERVERS"))?;
    let service = non_empty_env("KAFKA_SASL_USERNAME")
        .or_else(|| non_empty_env("KAFKA_CLIENT_ID"))
        .unwrap_or_else(|| SERVICE_NAME.to_string());

    let mut principal = match non_empty_env("KAFKA_SASL_PASSWORD") {
        Some(password) => ServicePrincipal::scram_sha_512(service, password),
        None => ServicePrincipal::insecure_dev(service),
    };
    if let Some(mechanism) = non_empty_env("KAFKA_SASL_MECHANISM") {
        principal.mechanism = mechanism;
    }
    if let Some(protocol) = non_empty_env("KAFKA_SECURITY_PROTOCOL") {
        principal.security_protocol = protocol;
    }

    let publisher = KafkaPublisher::new(&DataBusConfig::new(brokers, principal))?;
    Ok(publisher)
}

fn non_empty_env(key: &'static str) -> Option<String> {
    std::env::var(key).ok().and_then(|v| {
        let t = v.trim();
        if t.is_empty() {
            None
        } else {
            Some(t.to_string())
        }
    })
}
