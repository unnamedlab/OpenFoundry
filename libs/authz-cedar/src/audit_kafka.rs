//! Kafka-backed [`AuthzAuditSink`] (feature `kafka`).
//!
//! Implements the production wiring sketched in [`crate::audit`]: every
//! authorization decision is serialised as JSON and published to
//! `audit.authz.v1` via the data-plane bus. Two design points worth
//! pinning down:
//!
//! * **Fire-and-forget.** [`AuthzAuditSink::emit`] returns immediately:
//!   the actual `produce()` call runs on a detached `tokio::spawn` task
//!   so a slow broker can never stall the request hot path. Failures are
//!   swallowed at `tracing::warn` level — the engine has already made
//!   its decision and the request must proceed.
//! * **Partition-by-principal.** The Kafka record key is the principal
//!   EntityUid string. This guarantees that all decisions for a given
//!   user land on the same partition, which keeps downstream sinks
//!   (Iceberg, audit-sink) able to reconstruct a per-user timeline
//!   without a global sort.
//!
//! `KafkaAuthzAuditSink` is built around `Arc<dyn DataPublisher>` rather
//! than the concrete `KafkaPublisher` so the test-suite can swap in a
//! capturing mock without spinning up `librdkafka`. Production callers
//! pass `Arc::new(KafkaPublisher::from_env(...)?) as Arc<dyn _>`.

use std::sync::Arc;

use async_trait::async_trait;
use event_bus_data::{DataPublisher, OpenLineageHeaders};
use uuid::Uuid;

use crate::audit::{AuthzAuditEvent, AuthzAuditSink};

/// Canonical Kafka topic for authorization decisions.
///
/// Provisioned in
/// `infra/k8s/platform/manifests/strimzi/topics-domain-v1.yaml`
/// (12 partitions, RF=3, ISR=2).
pub const TOPIC: &str = "audit.authz.v1";

/// OpenLineage `namespace` attached to every emitted record.
const OL_NAMESPACE: &str = "of://authz";

/// OpenLineage `job.name` — every emission belongs to the logical
/// "decide" job of the embedded Cedar engine.
const OL_JOB_NAME: &str = "authz.decide";

/// OpenLineage `producer` URI.
const OL_PRODUCER: &str = "https://github.com/unnamedlab/OpenFoundry/libs/authz-cedar";

/// Fire-and-forget Kafka audit sink.
#[derive(Clone)]
pub struct KafkaAuthzAuditSink {
    publisher: Arc<dyn DataPublisher>,
    topic: String,
}

impl KafkaAuthzAuditSink {
    /// Build a sink that publishes to `topic` via `publisher`.
    pub fn new(publisher: Arc<dyn DataPublisher>, topic: String) -> Self {
        Self { publisher, topic }
    }

    /// Convenience: publish to the canonical [`TOPIC`].
    pub fn for_default_topic(publisher: Arc<dyn DataPublisher>) -> Self {
        Self::new(publisher, TOPIC.to_string())
    }

    /// Topic this sink writes to (exposed for tests / metrics).
    pub fn topic(&self) -> &str {
        &self.topic
    }
}

#[async_trait]
impl AuthzAuditSink for KafkaAuthzAuditSink {
    async fn emit(&self, event: AuthzAuditEvent) {
        let publisher = Arc::clone(&self.publisher);
        let topic = self.topic.clone();
        tokio::spawn(async move {
            let payload = match serde_json::to_vec(&event) {
                Ok(bytes) => bytes,
                Err(err) => {
                    tracing::warn!(
                        target: "authz.audit.kafka",
                        error = %err,
                        principal = %event.principal,
                        action = %event.action,
                        "failed to serialise AuthzAuditEvent — dropping"
                    );
                    return;
                }
            };

            let headers = OpenLineageHeaders::new(
                OL_NAMESPACE,
                OL_JOB_NAME,
                Uuid::new_v4().to_string(),
                OL_PRODUCER,
            )
            .with_event_time(event.timestamp);

            let key = event.principal.as_bytes();
            if let Err(err) = publisher
                .publish(&topic, Some(key), &payload, &headers)
                .await
            {
                tracing::warn!(
                    target: "authz.audit.kafka",
                    error = %err,
                    topic = %topic,
                    principal = %event.principal,
                    action = %event.action,
                    decision = %event.decision,
                    "kafka publish failed for authz audit event — dropping"
                );
            }
        });
    }
}
