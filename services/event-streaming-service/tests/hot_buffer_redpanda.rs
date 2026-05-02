//! Bloque B1 — E2E for the Kafka-backed [`HotBuffer`].
//!
//! Boots an ephemeral Redpanda broker via `testcontainers`, builds a
//! [`KafkaHotBuffer`] against it, and exercises the contract:
//!   1. `ensure_topic` is idempotent (calling twice does not error).
//!   2. `publish` succeeds for both keyed and keyless events.
//!
//! The test is gated by the `kafka-it` feature *and* `#[ignore]` so a
//! bare `cargo test` does not require Docker. Run with:
//!
//! ```text
//! cargo test -p event-streaming-service --features kafka-it \
//!   --test hot_buffer_redpanda -- --ignored --nocapture
//! ```

#![cfg(feature = "kafka-it")]

use std::time::Duration;

use event_bus_data::config::{DataBusConfig, ServicePrincipal};
use event_streaming_service::domain::hot_buffer::{HotBuffer, KafkaHotBuffer};
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
async fn kafka_hot_buffer_round_trip_against_redpanda() {
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
    let principal = ServicePrincipal::insecure_dev("event-streaming-service-test".to_string());
    let cfg = DataBusConfig::new(bootstrap, principal);
    let buffer = KafkaHotBuffer::new(cfg).expect("build kafka hot buffer");

    let stream_id = Uuid::now_v7();

    // 1. ensure_topic — happy path
    timeout(Duration::from_secs(20), buffer.ensure_topic(stream_id, 3))
        .await
        .expect("ensure_topic must not time out")
        .expect("ensure_topic must succeed");

    // 1b. ensure_topic — idempotent
    timeout(Duration::from_secs(20), buffer.ensure_topic(stream_id, 3))
        .await
        .expect("ensure_topic must not time out (second call)")
        .expect("ensure_topic must succeed when topic already exists");

    // 2. publish — keyed
    timeout(
        Duration::from_secs(20),
        buffer.publish(stream_id, Some("evt-1"), b"{\"id\":\"evt-1\",\"value\":1}"),
    )
    .await
    .expect("keyed publish must not time out")
    .expect("keyed publish must succeed");

    // 2b. publish — keyless
    timeout(
        Duration::from_secs(20),
        buffer.publish(stream_id, None, b"{\"id\":\"evt-2\",\"value\":2}"),
    )
    .await
    .expect("keyless publish must not time out")
    .expect("keyless publish must succeed");

    assert_eq!(buffer.id(), "kafka");
}
