//! Tarea 9 — E2E harness for Schema Registry compatibility checks.
//!
//! Boots Apicurio Registry (Confluent-API compatible) via testcontainers,
//! registers an Avro schema for a topic, then registers a backwards-compatible
//! evolution and asserts the registry accepts it. This mirrors the path the
//! Kafka connector takes when validating that produced messages match a
//! pinned subject's compatibility level.
//!
//! Gated by `schema-registry-it` + `#[ignore]`.
//!
//! ```text
//! cargo test -p connector-management-service --features schema-registry-it \
//!   --test schema_registry_compat -- --ignored --nocapture
//! ```

#![cfg(feature = "schema-registry-it")]

use std::time::Duration;

use serde_json::json;
use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};

const APICURIO_IMAGE: &str = "apicurio/apicurio-registry-mem";
const APICURIO_TAG: &str = "2.6.4.Final";
const APICURIO_PORT: u16 = 8080;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker (Apicurio Schema Registry)"]
async fn schema_registry_accepts_backwards_compatible_evolution() {
    let container = GenericImage::new(APICURIO_IMAGE, APICURIO_TAG)
        .with_exposed_port(APICURIO_PORT.tcp())
        .with_wait_for(WaitFor::message_on_stdout("Apicurio Registry started"))
        .start()
        .await
        .expect("apicurio container must start");

    let host_port = container
        .get_host_port_ipv4(APICURIO_PORT)
        .await
        .expect("mapped apicurio port");

    // Apicurio exposes a Confluent-compatible API at /apis/ccompat/v7.
    let base = format!("http://127.0.0.1:{host_port}/apis/ccompat/v7");
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(10))
        .build()
        .expect("reqwest client");

    let subject = "openfoundry.it.events-value";
    let v1_schema = json!({
        "schema": serde_json::to_string(&json!({
            "type": "record",
            "name": "Event",
            "fields": [{"name": "id", "type": "string"}]
        })).unwrap()
    });
    let v2_schema = json!({
        "schema": serde_json::to_string(&json!({
            "type": "record",
            "name": "Event",
            "fields": [
                {"name": "id", "type": "string"},
                {"name": "ts", "type": ["null", "long"], "default": null}
            ]
        })).unwrap()
    });

    // Set BACKWARD compatibility on the subject.
    let _ = client
        .put(format!("{base}/config/{subject}"))
        .json(&json!({"compatibility": "BACKWARD"}))
        .send()
        .await
        .expect("set compatibility");

    // Register v1.
    let v1 = client
        .post(format!("{base}/subjects/{subject}/versions"))
        .json(&v1_schema)
        .send()
        .await
        .expect("register v1");
    assert!(
        v1.status().is_success(),
        "v1 registration failed: {}",
        v1.status()
    );

    // Test v2 compatibility.
    let compat = client
        .post(format!(
            "{base}/compatibility/subjects/{subject}/versions/latest"
        ))
        .json(&v2_schema)
        .send()
        .await
        .expect("compat check");
    assert!(
        compat.status().is_success(),
        "compat HTTP error: {}",
        compat.status()
    );
    let body: serde_json::Value = compat.json().await.expect("compat body");
    assert_eq!(
        body.get("is_compatible").and_then(|v| v.as_bool()),
        Some(true),
        "v2 should be backwards-compatible: {body}"
    );
}
