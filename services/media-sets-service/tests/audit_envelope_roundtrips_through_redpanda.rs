//! H3 closure (optional) — wire-format compatibility check between the
//! `media-sets-service` envelope and the `audit-sink` decoder, exercised
//! end-to-end through an ephemeral Redpanda broker (testcontainer Kafka).
//!
//! Why this exists. The fast-path test
//! [`audit_events_emitted_on_upload_download_delete`] already proves
//! that handlers enqueue an [`audit_trail::events::AuditEnvelope`] into
//! `outbox.events`. This test closes the loop in the OPPOSITE direction:
//!
//!   1. Build the canonical envelope the way our handlers do.
//!   2. Publish it as a raw record to `audit.events.v1` on a real Kafka
//!      broker (Redpanda, the same image `event-streaming-service` uses
//!      for its CI integration tests).
//!   3. Consume the record from the topic and decode it with the
//!      `audit-sink` library — i.e. exactly what the audit pipeline does
//!      in production.
//!   4. Assert the envelope round-trips with the producer-side `kind`
//!      and the per-event payload intact.
//!
//! The test is `#[ignore]` and `#[cfg(feature = "kafka-it")]`-gated so
//! `cargo test -p media-sets-service` stays Docker-free. Opt in with:
//!
//! ```text
//! cargo test -p media-sets-service --features kafka-it \
//!   --test audit_envelope_roundtrips_through_redpanda \
//!   -- --ignored --nocapture
//! ```

#![cfg(feature = "kafka-it")]

use std::time::Duration;

use audit_trail::events::{AuditContext, AuditEnvelope, AuditEvent, TOPIC};
use audit_sink::{AuditEnvelope as SinkEnvelope, decode};
use chrono::Utc;
use rdkafka::admin::{AdminClient, AdminOptions, NewTopic, TopicReplication};
use rdkafka::client::DefaultClientContext;
use rdkafka::config::ClientConfig;
use rdkafka::consumer::{Consumer, StreamConsumer};
use rdkafka::producer::{FutureProducer, FutureRecord};
use rdkafka::{Message, TopicPartitionList};
use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};
use tokio::time::timeout;
use uuid::Uuid;

const REDPANDA_IMAGE: &str = "redpandadata/redpanda";
const REDPANDA_TAG: &str = "v23.3.5";
const KAFKA_PORT: u16 = 9092;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker (Redpanda)"]
async fn audit_envelope_round_trips_through_audit_events_v1_topic() {
    // ── 1. Spin up Redpanda ──────────────────────────────────────
    let container = GenericImage::new(REDPANDA_IMAGE, REDPANDA_TAG)
        .with_exposed_port(KAFKA_PORT.tcp())
        .with_wait_for(WaitFor::message_on_stderr("Successfully started Redpanda!"))
        .with_cmd([
            "redpanda",
            "start",
            "--overprovisioned",
            "--smp",
            "1",
            "--memory",
            "512M",
            "--reserve-memory",
            "0M",
            "--node-id",
            "0",
            "--check=false",
            "--kafka-addr",
            "PLAINTEXT://0.0.0.0:9092",
            "--advertise-kafka-addr",
            "PLAINTEXT://127.0.0.1:9092",
        ])
        .start()
        .await
        .expect("redpanda container must start");

    let host_port = container
        .get_host_port_ipv4(KAFKA_PORT)
        .await
        .expect("mapped Kafka port");
    let bootstrap = format!("127.0.0.1:{host_port}");

    // ── 2. Provision the canonical audit topic ────────────────────
    let admin: AdminClient<DefaultClientContext> = ClientConfig::new()
        .set("bootstrap.servers", &bootstrap)
        .create()
        .expect("admin client");
    admin
        .create_topics(
            &[NewTopic::new(TOPIC, 1, TopicReplication::Fixed(1))],
            &AdminOptions::new(),
        )
        .await
        .expect("create audit.events.v1 topic");

    // ── 3. Build the envelope the producer-side helper would build
    //       and publish it as a raw record (Debezium would normally
    //       do this from the outbox WAL).
    let event = AuditEvent::MediaItemUploaded {
        resource_rid: "ri.foundry.main.media_item.kafka-roundtrip".into(),
        media_set_rid: "ri.foundry.main.media_set.kafka-roundtrip".into(),
        project_rid: "ri.foundry.main.project.audit".into(),
        markings_at_event: vec!["public".into()],
        path: "kafka/roundtrip.png".into(),
        mime_type: "image/png".into(),
        size_bytes: 4096,
        sha256: "f".repeat(64),
        transaction_rid: None,
    };
    let ctx = AuditContext::for_actor("kafka-it")
        .with_request_id(Uuid::now_v7().to_string())
        .with_source_service("media-sets-service");
    let envelope = AuditEnvelope::build(&event, &ctx, Utc::now());
    let payload = serde_json::to_vec(&envelope).expect("serialize producer envelope");

    let producer: FutureProducer = ClientConfig::new()
        .set("bootstrap.servers", &bootstrap)
        .set("message.timeout.ms", "5000")
        .create()
        .expect("producer");
    let key = envelope.event_id.to_string();
    producer
        .send(
            FutureRecord::to(TOPIC).payload(&payload).key(&key),
            Duration::from_secs(5),
        )
        .await
        .expect("publish to audit.events.v1");

    // ── 4. Consume the record back and decode with audit-sink. ────
    let consumer: StreamConsumer = ClientConfig::new()
        .set("bootstrap.servers", &bootstrap)
        .set("group.id", "audit-roundtrip-test")
        .set("enable.auto.commit", "false")
        .set("auto.offset.reset", "earliest")
        .create()
        .expect("consumer");
    let mut tpl = TopicPartitionList::new();
    tpl.add_partition(TOPIC, 0);
    consumer.assign(&tpl).expect("assign");

    let received = timeout(Duration::from_secs(10), consumer.recv())
        .await
        .expect("timed out waiting for audit envelope")
        .expect("kafka recv");

    let bytes = received
        .payload()
        .expect("envelope record must carry payload bytes");
    // The audit-sink decoder is the source of truth for the wire
    // format. If this parses, every consumer downstream of the topic
    // will parse the same envelope without bespoke shim code.
    let sink_envelope: SinkEnvelope = decode(bytes).expect("audit-sink decodes media envelope");

    assert_eq!(sink_envelope.event_id, envelope.event_id);
    assert_eq!(sink_envelope.kind, "media_item.uploaded");
    // Producer- and consumer-side Unix-micro timestamps must agree.
    assert_eq!(sink_envelope.at, envelope.at);
    // The payload survived the round-trip — including the
    // discriminated union body, not just the top-level `kind`.
    assert_eq!(
        sink_envelope.payload.get("path"),
        Some(&serde_json::json!("kafka/roundtrip.png"))
    );
    assert_eq!(
        sink_envelope.correlation_id,
        envelope.correlation_id
    );
}
