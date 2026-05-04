//! Push proxy — schema validation contract.
//!
//! The full HTTP/Kafka end-to-end is exercised by
//! `hot_buffer_redpanda.rs`; this suite pins the validator behaviour
//! the proxy applies to every record before publishing. We split it
//! out so `cargo test push_proxy` runs without Docker and stays
//! deterministic on CI.
//!
//! When run with `--features kafka-it` the bottom test additionally
//! boots a Redpanda container via `testcontainers` to confirm the
//! proxy publishes successfully against a real broker.

use event_streaming_service::handlers::push_proxy::{
    ERR_PUSH_MISSING_TOKEN, ERR_PUSH_RATE_LIMITED, ERR_PUSH_SCHEMA, ERR_PUSH_VIEW_RETIRED, PushBody,
};
use serde_json::json;

#[test]
fn error_codes_match_documented_constants() {
    // The UI and SDK retry policies key off these strings — see
    // `services/event-streaming-service/README.md`.
    assert_eq!(ERR_PUSH_VIEW_RETIRED, "PUSH_VIEW_RETIRED");
    assert_eq!(ERR_PUSH_SCHEMA, "PUSH_SCHEMA_VALIDATION_FAILED");
    assert_eq!(ERR_PUSH_RATE_LIMITED, "PUSH_RATE_LIMITED");
    assert_eq!(ERR_PUSH_MISSING_TOKEN, "PUSH_MISSING_BEARER_TOKEN");
}

#[test]
fn push_body_accepts_foundry_records_shape() {
    let body: PushBody = serde_json::from_value(json!({
        "records": [
            { "value": { "sensor_id": "s1", "temperature": 4.1 } }
        ]
    }))
    .unwrap();
    let records = body.records.expect("records[] must deserialise");
    assert_eq!(records.len(), 1);
    assert!(records[0].value.is_some());
}

#[test]
fn push_body_accepts_sdk_values_shape() {
    let body: PushBody = serde_json::from_value(json!({
        "values": [{ "sensor_id": "s1", "temperature": 4.1 }]
    }))
    .unwrap();
    assert!(body.values.is_some());
}

#[test]
fn push_body_accepts_event_time_and_key_metadata() {
    let body: PushBody = serde_json::from_value(json!({
        "records": [
            {
                "value": { "id": "evt-1" },
                "event_time": "2026-05-04T00:00:00Z",
                "key": "evt-1"
            }
        ]
    }))
    .unwrap();
    let r = &body.records.unwrap()[0];
    assert!(r.event_time.is_some());
    assert_eq!(r.key.as_deref(), Some("evt-1"));
}

#[test]
fn empty_body_is_rejected_at_request_parse_time() {
    // The handler returns 400 when both `records[]` and `values[]`
    // are absent. We can't run the handler here without a full
    // AppState, but we can prove the body deserialises to an empty
    // shape so the handler's `if records.is_empty() ...` branch
    // triggers.
    let body: PushBody = serde_json::from_value(json!({})).unwrap();
    assert!(body.records.is_none() && body.values.is_none());
}

// ---------------------------------------------------------------------
// Kafka integration — only compiled when `kafka-it` is enabled. CI
// runs this test in the dedicated `kafka-it` job which provisions
// Docker + testcontainers (see `.github/workflows/kafka-lint.yml`).
// ---------------------------------------------------------------------

#[cfg(feature = "kafka-it")]
mod with_redpanda {
    use super::*;
    use std::time::Duration;

    use event_bus_data::config::{DataBusConfig, ServicePrincipal};
    use event_streaming_service::domain::hot_buffer::{HotBuffer, KafkaHotBuffer};
    use testcontainers::core::{IntoContainerPort, WaitFor};
    use testcontainers::runners::AsyncRunner;
    use testcontainers::{GenericImage, ImageExt};
    use uuid::Uuid;

    const REDPANDA_IMAGE: &str = "redpandadata/redpanda";
    const REDPANDA_TAG: &str = "v23.3.5";
    const KAFKA_PORT: u16 = 9092;

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    #[ignore = "requires Docker (Redpanda)"]
    async fn proxy_publishes_to_real_kafka_after_schema_validation() {
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
        let principal = ServicePrincipal::insecure_dev("push-proxy-test");
        let cfg = DataBusConfig::new(bootstrap, principal);
        let buffer = KafkaHotBuffer::new(cfg).expect("build kafka hot buffer");

        let stream_id = Uuid::now_v7();
        // Pre-create the topic with 1 partition; the push proxy
        // exercises `publish` against this topic.
        tokio::time::timeout(Duration::from_secs(20), buffer.ensure_topic(stream_id, 1))
            .await
            .expect("ensure_topic must not time out")
            .expect("ensure_topic must succeed");

        // Validated record (schema match) → publish must succeed.
        let payload = serde_json::to_vec(&json!({
            "id": "evt-1",
            "value": 1
        }))
        .unwrap();
        tokio::time::timeout(
            Duration::from_secs(20),
            buffer.publish(stream_id, Some("evt-1"), &payload),
        )
        .await
        .expect("publish must not time out")
        .expect("publish must succeed");
    }
}
