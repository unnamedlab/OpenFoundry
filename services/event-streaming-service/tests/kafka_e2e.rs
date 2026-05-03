//! End-to-end integration test for the real Kafka backend.
//!
//! This test boots an ephemeral Apache Kafka 3.7 broker via `testcontainers`
//! (reusing [`event_bus_data::testkit::EphemeralKafka`]), publishes a few
//! envelopes through [`RdKafkaBackend::publish`] and reads them back through
//! [`RdKafkaBackend::subscribe`]. It exercises:
//!
//! - producer wiring (`acks=all`, idempotent producer, headers attached);
//! - subscriber wiring (regex pattern translation, header round-trip,
//!   `schema_id` propagation under `x-of-schema-id`);
//! - graceful shutdown (dropping the stream releases the consumer).
//!
//! The test is gated by the `kafka-it` feature so default `cargo test` runs
//! do not require Docker. Run it with:
//!
//! ```text
//! cargo test -p event-streaming-service --features kafka-it --test kafka_e2e -- --nocapture
//! ```

#![cfg(feature = "kafka-it")]

use std::collections::BTreeMap;
use std::time::Duration;

use bytes::Bytes;
use event_bus_data::testkit::EphemeralKafka;
use event_streaming_service::backends::{Backend, Envelope, RdKafkaBackend};
use tokio_stream::StreamExt;

const SERVICE: &str = "event-streaming-it";

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn publish_and_subscribe_roundtrip() {
    let broker = EphemeralKafka::start()
        .await
        .expect("ephemeral Kafka broker must start (Docker required)");

    // Topic must be pre-created: the producer config disables auto-create.
    let topic = "openfoundry.it.roundtrip";
    broker.create_topic(topic, 1).await.expect("create topic");

    let bus_cfg = broker.config_for(SERVICE);
    let backend = RdKafkaBackend::new(bus_cfg, format!("{SERVICE}-router"))
        .expect("rdkafka backend must build");

    // Subscribe BEFORE publishing so we don't race the broker on offsets.
    // Use a literal-topic pattern to skip regex translation logic here; a
    // separate test below covers wildcards.
    let mut stream = backend
        .subscribe(topic)
        .await
        .expect("subscribe must succeed");

    let mut headers = BTreeMap::new();
    headers.insert("ol-namespace".to_string(), "of://test".to_string());
    headers.insert("ol-job-name".to_string(), "kafka-e2e".to_string());

    for i in 0..3u8 {
        let envelope = Envelope {
            topic: topic.to_string(),
            payload: Bytes::from(vec![i, i + 1, i + 2]),
            headers: headers.clone(),
            schema_id: Some(format!("schema-{i}")),
        };
        backend
            .publish(envelope)
            .await
            .expect("publish must be acknowledged");
    }

    // Drain three records with a generous timeout so a slow CI host does not
    // produce flakes.
    for expected in 0..3u8 {
        let next = tokio::time::timeout(Duration::from_secs(15), stream.next()).await;
        let item = next
            .expect("must receive a record before timeout")
            .expect("stream must yield Some")
            .expect("envelope must be Ok");
        assert_eq!(item.topic, topic);
        assert_eq!(
            item.payload.as_ref(),
            &[expected, expected + 1, expected + 2]
        );
        assert_eq!(
            item.schema_id.as_deref(),
            Some(format!("schema-{expected}").as_str())
        );
        // Caller-provided headers must round-trip verbatim.
        assert_eq!(
            item.headers.get("ol-namespace").map(String::as_str),
            Some("of://test")
        );
        assert_eq!(
            item.headers.get("ol-job-name").map(String::as_str),
            Some("kafka-e2e")
        );
        // The auto-stamped `ol-event-time` must be present.
        assert!(
            item.headers.contains_key("ol-event-time"),
            "ol-event-time must be auto-stamped on publish, got headers: {:?}",
            item.headers
        );
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn subscribe_with_wildcard_translates_to_kafka_regex() {
    let broker = EphemeralKafka::start()
        .await
        .expect("ephemeral Kafka broker must start");

    // Two siblings matched by a single-token wildcard.
    broker
        .create_topic("data.alpha", 1)
        .await
        .expect("create alpha");
    broker
        .create_topic("data.beta", 1)
        .await
        .expect("create beta");
    // A non-match that must NOT show up.
    broker
        .create_topic("other.gamma", 1)
        .await
        .expect("create gamma");

    let backend = RdKafkaBackend::new(broker.config_for(SERVICE), format!("{SERVICE}-wildcard"))
        .expect("backend builds");

    let mut stream = backend
        .subscribe("data.*")
        .await
        .expect("wildcard subscribe must succeed");

    // Give the consumer a moment to be assigned before publishing.
    tokio::time::sleep(Duration::from_secs(1)).await;

    for topic in ["data.alpha", "data.beta", "other.gamma"] {
        let envelope = Envelope {
            topic: topic.to_string(),
            payload: Bytes::from_static(b"x"),
            headers: BTreeMap::new(),
            schema_id: None,
        };
        backend.publish(envelope).await.expect("publish ok");
    }

    let mut received = Vec::new();
    let deadline = tokio::time::Instant::now() + Duration::from_secs(15);
    while received.len() < 2 {
        let remaining = deadline.saturating_duration_since(tokio::time::Instant::now());
        if remaining.is_zero() {
            break;
        }
        if let Ok(Some(Ok(env))) = tokio::time::timeout(remaining, stream.next()).await {
            received.push(env.topic);
        }
    }
    received.sort();
    assert_eq!(
        received,
        vec!["data.alpha".to_string(), "data.beta".to_string()]
    );
}
