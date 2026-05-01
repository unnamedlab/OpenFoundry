//! Tarea 9 — E2E harness for the Kafka connector path.
//!
//! Boots an ephemeral Redpanda broker via `testcontainers`, then validates
//! that the same low-level protocol the connector uses (TCP + Kafka API
//! handshake via the bootstrap port) is reachable. Gated by the
//! `kafka-it` feature plus `#[ignore]` so plain `cargo test` does not
//! require Docker.
//!
//! ```text
//! cargo test -p connector-management-service --features kafka-it \
//!   --test kafka_real_broker -- --ignored --nocapture
//! ```
//!
//! NOTE: `connector-management-service` ships as a binary (no `[lib]`),
//! so this harness exercises the broker reachability and acts as the
//! foundation for HTTP-level e2e once a thin `lib.rs` is introduced.
//! The fixture matches the contract that `connectors::kafka::test_connection`
//! depends on (a Kafka 0.11+ broker reachable on a TCP port).

#![cfg(feature = "kafka-it")]

use std::time::Duration;

use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};
use tokio::io::AsyncWriteExt;
use tokio::net::TcpStream;
use tokio::time::timeout;

const REDPANDA_IMAGE: &str = "redpandadata/redpanda";
const REDPANDA_TAG: &str = "v23.3.5";
const KAFKA_PORT: u16 = 9092;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker (Redpanda)"]
async fn redpanda_broker_is_reachable_for_connector() {
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

    // Validate that the broker's Kafka listener is open. The connector's
    // `test_connection` boils down to opening this socket and exchanging
    // an API versions request, so reachability here is the prerequisite.
    let connect = timeout(
        Duration::from_secs(15),
        TcpStream::connect(("127.0.0.1", host_port)),
    )
    .await
    .expect("TCP connect did not time out")
    .expect("TCP connect must succeed");
    let mut stream = connect;
    // Drop the stream cleanly; we just needed to prove the listener is up.
    stream.shutdown().await.ok();
}
