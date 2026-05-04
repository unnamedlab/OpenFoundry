//! End-to-end test for [`KafkaAuthzAuditSink`] (feature `kafka`).
//!
//! We do not spin up a real Kafka broker here — that would require
//! Docker and is covered by the `event-bus-data` `it` suite. Instead we
//! plug a `CapturingPublisher` into the sink, drive a single
//! `AuthzEngine::authorize` call, and assert that exactly one record
//! lands on `audit.authz.v1` with the expected JSON payload, partition
//! key and `ol-*` headers.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use async_trait::async_trait;
use authz_cedar::audit::AuthzAuditSink;
use authz_cedar::{AuthzEngine, KAFKA_AUDIT_TOPIC, KafkaAuthzAuditSink, PolicyRecord, PolicyStore};
use cedar_policy::{Context, Entities, EntityUid};
use event_bus_data::{DataPublisher, OpenLineageHeaders, PublishError};
use serde_json::Value;

/// Captured Kafka record. Mirrors the `(topic, key, payload, headers)`
/// tuple `KafkaPublisher::publish` would have sent on the wire.
#[derive(Debug, Clone)]
struct CapturedRecord {
    topic: String,
    key: Option<Vec<u8>>,
    payload: Vec<u8>,
    headers: OpenLineageHeaders,
}

/// In-memory `DataPublisher` that appends every published record to a
/// shared `Vec`. Tests inspect the vector after `emit` has had a chance
/// to run on the detached audit task.
#[derive(Default, Clone)]
struct CapturingPublisher {
    records: Arc<Mutex<Vec<CapturedRecord>>>,
}

impl CapturingPublisher {
    fn snapshot(&self) -> Vec<CapturedRecord> {
        self.records.lock().expect("publisher mutex").clone()
    }
}

#[async_trait]
impl DataPublisher for CapturingPublisher {
    async fn publish(
        &self,
        topic: &str,
        key: Option<&[u8]>,
        payload: &[u8],
        lineage: &OpenLineageHeaders,
    ) -> Result<(), PublishError> {
        self.records
            .lock()
            .expect("publisher mutex")
            .push(CapturedRecord {
                topic: topic.to_string(),
                key: key.map(|k| k.to_vec()),
                payload: payload.to_vec(),
                headers: lineage.clone(),
            });
        Ok(())
    }

    async fn flush(&self, _timeout: Duration) -> Result<(), PublishError> {
        Ok(())
    }
}

/// One permissive policy keeps the test focused on the audit emission
/// rather than on cedar evaluation: any user can read any dataset.
const TEST_POLICY: &str = r#"
    permit(
      principal,
      action == Action::"read",
      resource is Dataset
    );
"#;

async fn engine_with_capturing_sink() -> (AuthzEngine, CapturingPublisher) {
    let store = PolicyStore::with_policies(&[PolicyRecord {
        id: "test-allow-read".into(),
        version: 1,
        description: None,
        source: TEST_POLICY.into(),
    }])
    .await
    .expect("policy compiles against bundled schema");

    let publisher = CapturingPublisher::default();
    let sink = KafkaAuthzAuditSink::for_default_topic(
        Arc::new(publisher.clone()) as Arc<dyn DataPublisher>
    );
    let engine = AuthzEngine::new(store, Arc::new(sink));
    (engine, publisher)
}

/// Pump the tokio runtime until the detached audit task has had a
/// chance to publish. Two layers of `tokio::spawn` separate
/// `engine.authorize` from `publisher.publish` (engine -> sink -> task)
/// so a single `tokio::task::yield_now()` is not enough; a short sleep
/// keeps the test deterministic without coupling to internal task ids.
async fn drain_detached_emit(publisher: &CapturingPublisher) -> Vec<CapturedRecord> {
    for _ in 0..50 {
        tokio::time::sleep(Duration::from_millis(10)).await;
        let snapshot = publisher.snapshot();
        if !snapshot.is_empty() {
            return snapshot;
        }
    }
    publisher.snapshot()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn authorize_emits_exactly_one_kafka_record_with_expected_shape() {
    let (engine, publisher) = engine_with_capturing_sink().await;

    let principal: EntityUid = r#"User::"alice""#.parse().expect("principal parses");
    let action: EntityUid = r#"Action::"read""#.parse().expect("action parses");
    let resource: EntityUid = r#"Dataset::"ds-1""#.parse().expect("resource parses");

    let entities = Entities::empty();
    let outcome = engine
        .authorize(principal, action, resource, Context::empty(), &entities)
        .await
        .expect("authorize returns");
    assert!(outcome.is_allow(), "permissive policy should allow read");

    let captured = drain_detached_emit(&publisher).await;
    assert_eq!(
        captured.len(),
        1,
        "one and only one audit record per decision"
    );

    let rec = &captured[0];
    assert_eq!(rec.topic, KAFKA_AUDIT_TOPIC);
    assert_eq!(rec.topic, "audit.authz.v1");

    // Partition key is the principal EntityUid string so per-user
    // timelines are reconstructable downstream without a global sort.
    assert_eq!(
        rec.key.as_deref().expect("partition key present"),
        b"User::\"alice\"",
    );

    let payload: Value = serde_json::from_slice(&rec.payload).expect("payload is JSON");
    assert_eq!(payload["principal"], "User::\"alice\"");
    assert_eq!(payload["action"], "Action::\"read\"");
    assert_eq!(payload["resource"], "Dataset::\"ds-1\"");
    assert_eq!(payload["decision"], "allow");
    assert!(
        payload.get("timestamp").and_then(|v| v.as_str()).is_some(),
        "timestamp is serialised as RFC3339 string",
    );

    // OpenLineage headers are populated and the `ol-event-time` tracks
    // the decision timestamp surfaced in the payload.
    assert_eq!(rec.headers.namespace, "of://authz");
    assert_eq!(rec.headers.job_name, "authz.decide");
    assert!(
        !rec.headers.run_id.is_empty(),
        "run_id acts as correlation id"
    );
    let payload_ts = payload["timestamp"].as_str().expect("timestamp string");
    let header_ts = rec.headers.event_time.to_rfc3339();
    assert_eq!(
        payload_ts, header_ts,
        "event_time mirrors the decision timestamp"
    );
}
