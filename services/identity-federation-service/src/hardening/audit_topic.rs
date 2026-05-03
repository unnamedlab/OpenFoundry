//! S3.1.g — Audit topic + event DTOs.
//!
//! All identity events publish to Kafka topic `audit.identity.v1`.
//! Schema is JSON-encoded. Runtime wiring is configurable: fail-open
//! is the default for auth availability, and fail-closed is available
//! for environments where audit durability is mandatory.

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use async_trait::async_trait;
use axum::http::HeaderMap;
use chrono::{DateTime, Utc};
use event_bus_data::{DataPublisher, OpenLineageHeaders};
use serde::{Deserialize, Serialize};
use thiserror::Error;
use uuid::Uuid;

/// Kafka topic for identity audit events.
pub const TOPIC: &str = "audit.identity.v1";

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum IdentityAuditEvent {
    Login {
        user_id: String,
        ip: String,
        method: String,
        outcome: AuditOutcome,
    },
    Logout {
        user_id: String,
    },
    MfaChallenge {
        user_id: String,
        factor: String,
        outcome: MfaOutcome,
    },
    KeyRotation {
        old_kid: String,
        new_kid: String,
        actor: String,
    },
    PasswordReset {
        user_id: String,
        actor: String,
    },
    RefreshTokenReplay {
        user_id: String,
        family_id: Uuid,
    },
    ScimUserProvisioned {
        user_id: String,
        actor: String,
        external_id: Option<String>,
    },
    ScimGroupProvisioned {
        group_id: String,
        actor: String,
        external_id: Option<String>,
    },
    SessionIssued {
        user_id: String,
        session_id: Option<Uuid>,
        method: String,
    },
    SessionRevoked {
        user_id: String,
        session_id: Option<Uuid>,
        actor: String,
    },
    TokenRefresh {
        user_id: String,
        token_id: Uuid,
        outcome: AuditOutcome,
    },
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum AuditOutcome {
    Success,
    Failure,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum MfaOutcome {
    Pass,
    Fail,
    Lockout,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditEnvelope {
    pub event_id: Uuid,
    pub at: DateTime<Utc>,
    pub correlation_id: Uuid,
    pub actor: Option<String>,
    pub payload: IdentityAuditEvent,
}

impl AuditEnvelope {
    pub fn new(correlation_id: Uuid, actor: Option<String>, payload: IdentityAuditEvent) -> Self {
        Self {
            event_id: Uuid::now_v7(),
            at: Utc::now(),
            correlation_id,
            actor,
            payload,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AuditFailurePolicy {
    FailOpen,
    FailClosed,
}

impl AuditFailurePolicy {
    pub fn from_env() -> Self {
        match std::env::var("IDENTITY_AUDIT_FAILURE_POLICY")
            .unwrap_or_else(|_| "fail_open".into())
            .to_ascii_lowercase()
            .as_str()
        {
            "fail_closed" | "blocking" | "required" => Self::FailClosed,
            _ => Self::FailOpen,
        }
    }
}

#[derive(Debug, Error)]
pub enum AuditPublishError {
    #[error("audit serialization failed: {0}")]
    Serialize(String),
    #[error("audit bus publish failed: {0}")]
    Publish(String),
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct AuditMetricsSnapshot {
    pub attempts: u64,
    pub published: u64,
    pub failed: u64,
    pub dropped: u64,
}

#[derive(Debug, Default)]
pub struct AuditMetrics {
    attempts: AtomicU64,
    published: AtomicU64,
    failed: AtomicU64,
    dropped: AtomicU64,
}

impl AuditMetrics {
    pub fn snapshot(&self) -> AuditMetricsSnapshot {
        AuditMetricsSnapshot {
            attempts: self.attempts.load(Ordering::Relaxed),
            published: self.published.load(Ordering::Relaxed),
            failed: self.failed.load(Ordering::Relaxed),
            dropped: self.dropped.load(Ordering::Relaxed),
        }
    }

    fn record_attempt(&self) {
        self.attempts.fetch_add(1, Ordering::Relaxed);
    }

    fn record_published(&self) {
        self.published.fetch_add(1, Ordering::Relaxed);
    }

    fn record_failed(&self) {
        self.failed.fetch_add(1, Ordering::Relaxed);
    }

    fn record_dropped(&self) {
        self.dropped.fetch_add(1, Ordering::Relaxed);
    }
}

#[async_trait]
pub trait IdentityAuditPublisher: Send + Sync {
    async fn publish(&self, envelope: &AuditEnvelope) -> Result<(), AuditPublishError>;
}

#[derive(Clone)]
pub struct KafkaIdentityAuditPublisher {
    publisher: Arc<dyn DataPublisher>,
    topic: String,
    producer: String,
    schema_url: Option<String>,
}

impl KafkaIdentityAuditPublisher {
    pub fn new(publisher: Arc<dyn DataPublisher>) -> Self {
        Self {
            publisher,
            topic: TOPIC.into(),
            producer: "pkg:openfoundry/identity-federation-service".into(),
            schema_url: Some("apicurio://openfoundry/audit.identity.v1".into()),
        }
    }
}

#[async_trait]
impl IdentityAuditPublisher for KafkaIdentityAuditPublisher {
    async fn publish(&self, envelope: &AuditEnvelope) -> Result<(), AuditPublishError> {
        let payload = serde_json::to_vec(envelope)
            .map_err(|error| AuditPublishError::Serialize(error.to_string()))?;
        let headers = audit_headers(envelope, &self.producer, self.schema_url.as_deref());
        self.publisher
            .publish(
                &self.topic,
                Some(envelope.event_id.as_bytes()),
                &payload,
                &headers,
            )
            .await
            .map_err(|error| AuditPublishError::Publish(error.to_string()))
    }
}

#[derive(Clone)]
pub struct IdentityAuditService {
    publisher: Option<Arc<dyn IdentityAuditPublisher>>,
    policy: AuditFailurePolicy,
    metrics: Arc<AuditMetrics>,
}

impl IdentityAuditService {
    pub fn disabled() -> Self {
        Self {
            publisher: None,
            policy: AuditFailurePolicy::FailOpen,
            metrics: Arc::new(AuditMetrics::default()),
        }
    }

    pub fn new(
        publisher: Arc<dyn IdentityAuditPublisher>,
        policy: AuditFailurePolicy,
        metrics: Arc<AuditMetrics>,
    ) -> Self {
        Self {
            publisher: Some(publisher),
            policy,
            metrics,
        }
    }

    pub fn metrics(&self) -> AuditMetricsSnapshot {
        self.metrics.snapshot()
    }

    pub async fn record(
        &self,
        correlation_id: Uuid,
        actor: Option<String>,
        payload: IdentityAuditEvent,
    ) -> Result<(), AuditPublishError> {
        self.metrics.record_attempt();
        let envelope = AuditEnvelope::new(correlation_id, actor, payload);
        let Some(publisher) = &self.publisher else {
            self.metrics.record_dropped();
            return Ok(());
        };
        match publisher.publish(&envelope).await {
            Ok(()) => {
                self.metrics.record_published();
                Ok(())
            }
            Err(error) => {
                self.metrics.record_failed();
                tracing::error!(%error, event_id = %envelope.event_id, correlation_id = %envelope.correlation_id, "identity audit publish failed");
                match self.policy {
                    AuditFailurePolicy::FailOpen => Ok(()),
                    AuditFailurePolicy::FailClosed => Err(error),
                }
            }
        }
    }
}

pub fn audit_headers(
    envelope: &AuditEnvelope,
    producer: &str,
    schema_url: Option<&str>,
) -> OpenLineageHeaders {
    let mut headers = OpenLineageHeaders::new(
        "openfoundry.identity",
        "identity-federation-service.audit",
        envelope.correlation_id.to_string(),
        producer,
    )
    .with_event_time(envelope.at);
    if let Some(schema_url) = schema_url {
        headers = headers.with_schema_url(schema_url);
    }
    headers
}

pub fn correlation_id_from_headers(headers: &HeaderMap) -> Uuid {
    headers
        .get("x-correlation-id")
        .or_else(|| headers.get("x-request-id"))
        .and_then(|value| value.to_str().ok())
        .and_then(|value| Uuid::parse_str(value).ok())
        .unwrap_or_else(Uuid::now_v7)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn login_event_round_trip() {
        let env = AuditEnvelope::new(
            Uuid::now_v7(),
            Some("u1".into()),
            IdentityAuditEvent::Login {
                user_id: "u1".into(),
                ip: "10.0.0.1".into(),
                method: "password".into(),
                outcome: AuditOutcome::Success,
            },
        );
        let s = serde_json::to_string(&env).unwrap();
        let back: AuditEnvelope = serde_json::from_str(&s).unwrap();
        assert_eq!(back.payload, env.payload);
    }

    #[test]
    fn topic_is_pinned() {
        assert_eq!(TOPIC, "audit.identity.v1");
    }

    struct MockPublisher {
        should_fail: bool,
        calls: Arc<AtomicU64>,
    }

    #[async_trait]
    impl IdentityAuditPublisher for MockPublisher {
        async fn publish(&self, _envelope: &AuditEnvelope) -> Result<(), AuditPublishError> {
            self.calls.fetch_add(1, Ordering::Relaxed);
            if self.should_fail {
                Err(AuditPublishError::Publish("boom".into()))
            } else {
                Ok(())
            }
        }
    }

    #[tokio::test]
    async fn fail_open_records_failure_without_returning_error() {
        let calls = Arc::new(AtomicU64::new(0));
        let metrics = Arc::new(AuditMetrics::default());
        let service = IdentityAuditService::new(
            Arc::new(MockPublisher {
                should_fail: true,
                calls: calls.clone(),
            }),
            AuditFailurePolicy::FailOpen,
            metrics.clone(),
        );
        let result = service
            .record(
                Uuid::now_v7(),
                Some("actor".into()),
                IdentityAuditEvent::Logout {
                    user_id: "u1".into(),
                },
            )
            .await;
        assert!(result.is_ok());
        assert_eq!(calls.load(Ordering::Relaxed), 1);
        assert_eq!(metrics.snapshot().failed, 1);
    }

    #[tokio::test]
    async fn fail_closed_returns_publisher_error() {
        let metrics = Arc::new(AuditMetrics::default());
        let service = IdentityAuditService::new(
            Arc::new(MockPublisher {
                should_fail: true,
                calls: Arc::new(AtomicU64::new(0)),
            }),
            AuditFailurePolicy::FailClosed,
            metrics,
        );
        let result = service
            .record(
                Uuid::now_v7(),
                Some("actor".into()),
                IdentityAuditEvent::Logout {
                    user_id: "u1".into(),
                },
            )
            .await;
        assert!(result.is_err());
    }

    #[test]
    fn correlation_id_is_propagated_as_lineage_run_header() {
        let correlation_id = Uuid::now_v7();
        let envelope = AuditEnvelope::new(
            correlation_id,
            Some("actor".into()),
            IdentityAuditEvent::Logout {
                user_id: "u1".into(),
            },
        );
        let headers = audit_headers(&envelope, "producer", Some("schema"));
        assert_eq!(headers.run_id, correlation_id.to_string());
        assert_eq!(headers.schema_url.as_deref(), Some("schema"));
    }
}
