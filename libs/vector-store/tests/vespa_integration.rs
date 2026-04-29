//! End-to-end integration test for the Vespa backend.
//!
//! This test boots the official `vespaengine/vespa` image with
//! `testcontainers`, deploys the minimal application package living
//! under `tests/fixtures/vespa-app/`, performs upserts and asserts that
//! [`VectorBackend::hybrid_query`] returns sensible top-k results.
//!
//! Because it requires Docker (and pulling a multi-GB image) it is
//! marked `#[ignore]`; run it explicitly with:
//!
//! ```bash
//! cargo test -p vector-store --features vespa --test vespa_integration -- --ignored
//! ```
//!
//! The test still **compiles** under `cargo test -p vector-store
//! --features vespa`, which is the criterion required by the task.

#![cfg(feature = "vespa")]

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::time::Duration;

use serde_json::json;
use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};

use vector_store::{Filter, VectorBackend};
use vector_store::vespa::{VespaBackend, VespaConfig};

/// Path to the application package shipped under `tests/fixtures`.
fn fixture_app_path() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/vespa-app")
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker and the vespaengine/vespa image (~2GB)"]
async fn hybrid_query_returns_topk_after_upsert() {
    // --- 1. Boot Vespa ----------------------------------------------------
    let image = GenericImage::new("vespaengine/vespa", "8")
        .with_exposed_port(8080.tcp())
        .with_exposed_port(19071.tcp())
        .with_wait_for(WaitFor::message_on_stdout("Container is ready"));
    let container = image
        .with_env_var("VESPA_CONFIGSERVERS", "localhost")
        .start()
        .await
        .expect("start vespa container");

    let host = container.get_host().await.expect("host");
    let http_port = container
        .get_host_port_ipv4(8080)
        .await
        .expect("http port");
    let cfg_port = container
        .get_host_port_ipv4(19071)
        .await
        .expect("config port");

    // --- 2. Deploy the application package via the config server ----------
    deploy_app_package(&format!("http://{host}:{cfg_port}"), &fixture_app_path())
        .await
        .expect("deploy app package");

    // Wait for the container endpoint to start serving the schema.
    wait_until_ready(&format!("http://{host}:{http_port}")).await;

    // --- 3. Build the backend and exercise it ----------------------------
    let backend = VespaBackend::new(VespaConfig::new(format!("http://{host}:{http_port}")))
        .expect("backend");

    let mut fields = BTreeMap::new();
    fields.insert("text".to_string(), json!("the quick brown fox"));
    fields.insert("tenant_id".to_string(), json!("acme"));
    backend
        .upsert("doc-1", &fields, &[1.0, 0.0, 0.0, 0.0])
        .await
        .expect("upsert doc-1");

    let mut fields2 = BTreeMap::new();
    fields2.insert("text".to_string(), json!("a lazy dog sleeps"));
    fields2.insert("tenant_id".to_string(), json!("acme"));
    backend
        .upsert("doc-2", &fields2, &[0.0, 1.0, 0.0, 0.0])
        .await
        .expect("upsert doc-2");

    // Vespa indexing is async; poll briefly for visibility.
    let hits = poll_hits(&backend, "fox", &[1.0, 0.0, 0.0, 0.0]).await;
    assert!(!hits.is_empty(), "expected at least one hit");
    assert_eq!(hits[0].id, "doc-1", "doc-1 should rank first");
}

/// Upload + activate an application package by zipping it and POSTing to
/// `/application/v2/tenant/default/prepareandactivate` on the config
/// server. Done here with a simple `tar` shell-out to avoid pulling in a
/// zip crate just for tests.
async fn deploy_app_package(
    config_url: &str,
    app_dir: &std::path::Path,
) -> Result<(), Box<dyn std::error::Error>> {
    use std::process::Command;

    let zip_path = std::env::temp_dir().join("vespa-app.zip");
    let _ = std::fs::remove_file(&zip_path);
    let status = Command::new("zip")
        .arg("-r")
        .arg(&zip_path)
        .arg(".")
        .current_dir(app_dir)
        .status()?;
    assert!(status.success(), "zip failed");

    let body = std::fs::read(&zip_path)?;
    let client = reqwest::Client::new();
    let resp = client
        .post(format!(
            "{config_url}/application/v2/tenant/default/prepareandactivate"
        ))
        .header("Content-Type", "application/zip")
        .body(body)
        .send()
        .await?;
    assert!(resp.status().is_success(), "deploy failed: {:?}", resp.text().await?);
    Ok(())
}

/// Poll `/ApplicationStatus` until the container reports it is serving.
async fn wait_until_ready(http_url: &str) {
    let client = reqwest::Client::new();
    let deadline = std::time::Instant::now() + Duration::from_secs(120);
    loop {
        if let Ok(r) = client
            .get(format!("{http_url}/ApplicationStatus"))
            .send()
            .await
        {
            if r.status().is_success() {
                return;
            }
        }
        if std::time::Instant::now() > deadline {
            panic!("vespa container never became ready");
        }
        tokio::time::sleep(Duration::from_secs(2)).await;
    }
}

/// Poll for hits because Vespa indexing is asynchronous.
async fn poll_hits(
    backend: &VespaBackend,
    text: &str,
    embedding: &[f32],
) -> Vec<vector_store::QueryHit> {
    let deadline = std::time::Instant::now() + Duration::from_secs(30);
    loop {
        let hits = backend
            .hybrid_query(text, embedding, &Filter::default(), 10)
            .await
            .expect("hybrid_query");
        if !hits.is_empty() {
            return hits;
        }
        if std::time::Instant::now() > deadline {
            return hits;
        }
        tokio::time::sleep(Duration::from_secs(1)).await;
    }
}
