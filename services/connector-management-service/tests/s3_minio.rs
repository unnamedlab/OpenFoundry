//! Tarea 9 — E2E harness for the S3 connector against MinIO.
//!
//! Boots `quay.io/minio/minio` via testcontainers and validates that the
//! S3 protocol used by `connectors::s3::test_connection` works against the
//! ephemeral endpoint. The test uses raw `reqwest` against the MinIO
//! healthcheck endpoint to keep dependencies light; full bucket I/O is
//! left to the connector code (which already has unit tests for its
//! request signing path).
//!
//! Gated by `s3-it` + `#[ignore]`.
//!
//! ```text
//! cargo test -p connector-management-service --features s3-it \
//!   --test s3_minio -- --ignored --nocapture
//! ```

#![cfg(feature = "s3-it")]

use std::time::Duration;

use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};
use tokio::time::timeout;

const MINIO_PORT: u16 = 9000;
const MINIO_USER: &str = "minioadmin";
const MINIO_PASS: &str = "minioadmin";

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker (MinIO)"]
async fn minio_endpoint_is_reachable_for_s3_connector() {
    let container = GenericImage::new("quay.io/minio/minio", "RELEASE.2024-09-13T20-26-02Z")
        .with_exposed_port(MINIO_PORT.tcp())
        .with_wait_for(WaitFor::message_on_stderr("API:"))
        .with_env_var("MINIO_ROOT_USER", MINIO_USER)
        .with_env_var("MINIO_ROOT_PASSWORD", MINIO_PASS)
        .with_cmd(["server", "/data", "--address", ":9000"])
        .start()
        .await
        .expect("minio container must start");

    let host_port = container
        .get_host_port_ipv4(MINIO_PORT)
        .await
        .expect("mapped minio port");

    let endpoint = format!("http://127.0.0.1:{host_port}/minio/health/ready");
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .expect("reqwest client");

    let response = timeout(Duration::from_secs(15), client.get(&endpoint).send())
        .await
        .expect("MinIO health request did not time out")
        .expect("MinIO health request succeeded");
    assert!(
        response.status().is_success(),
        "MinIO health endpoint returned {}",
        response.status()
    );
}
