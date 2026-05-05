//! ADR-0041 — productive `OutputTransactionClient` (the
//! [`pipeline_build_service::domain::iceberg_output_client::IcebergOutputClient`])
//! routes commit calls for Iceberg dataset RIDs to
//! `iceberg-catalog-service`'s `POST /iceberg/v1/transactions/commit`
//! endpoint.
//!
//! What this test pins:
//!
//! * Iceberg-prefixed RIDs (`ri.foundry.main.iceberg-table.<id>`) hit
//!   the catalog with `build_rid` set to the executor-supplied
//!   transaction RID, so the catalog's audit row carries the
//!   correlation back to the build.
//! * Non-Iceberg RIDs are silently skipped — the client is a
//!   single-responsibility router for Iceberg outputs and never
//!   pretends to commit anything else (a future composing wrapper
//!   handles the legacy datasets).
//! * Bearer credential, when configured, lands on the wire as
//!   `Authorization: Bearer …`.
//! * Catalog-side failure surfaces as `OutputClientError` so the build
//!   executor's multi-output rollback path fires.
//! * `abort` is always a no-op (the catalog rolls back atomically on
//!   commit failure; there is no separate abort endpoint).

use pipeline_build_service::domain::build_executor::OutputTransactionClient;
use pipeline_build_service::domain::iceberg_output_client::IcebergOutputClient;
use serde_json::json;
use wiremock::matchers::{body_partial_json, header, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

const ICEBERG_DATASET: &str = "ri.foundry.main.iceberg-table.00000000-0000-0000-0000-000000000001";
const TXN_RID: &str = "ri.foundry.main.transaction.42";

#[tokio::test]
async fn commit_routes_iceberg_dataset_to_catalog_multi_table_endpoint() {
    let server = MockServer::start().await;

    Mock::given(method("POST"))
        .and(path("/iceberg/v1/transactions/commit"))
        .and(body_partial_json(json!({ "build_rid": TXN_RID })))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({ "committed": [] })))
        .expect(1)
        .mount(&server)
        .await;

    let client = IcebergOutputClient::new(server.uri(), None, reqwest::Client::new());

    client
        .commit(ICEBERG_DATASET, TXN_RID)
        .await
        .expect("iceberg commit must succeed when catalog returns 200");
}

#[tokio::test]
async fn commit_forwards_bearer_token_when_configured() {
    let server = MockServer::start().await;

    Mock::given(method("POST"))
        .and(path("/iceberg/v1/transactions/commit"))
        .and(header("authorization", "Bearer ofty_test"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({ "committed": [] })))
        .expect(1)
        .mount(&server)
        .await;

    let client = IcebergOutputClient::new(
        server.uri(),
        Some("ofty_test".to_string()),
        reqwest::Client::new(),
    );

    client
        .commit(ICEBERG_DATASET, TXN_RID)
        .await
        .expect("commit with bearer must succeed");
}

#[tokio::test]
async fn commit_skips_non_iceberg_dataset_rids_without_calling_catalog() {
    let server = MockServer::start().await;

    Mock::given(method("POST"))
        .and(path("/iceberg/v1/transactions/commit"))
        .respond_with(ResponseTemplate::new(500))
        .expect(0)
        .mount(&server)
        .await;

    let client = IcebergOutputClient::new(server.uri(), None, reqwest::Client::new());

    client
        .commit("ri.foundry.main.dataset.legacy", TXN_RID)
        .await
        .expect("non-iceberg commit is a noop and must succeed");
}

#[tokio::test]
async fn commit_returns_output_client_error_on_catalog_failure() {
    let server = MockServer::start().await;

    Mock::given(method("POST"))
        .and(path("/iceberg/v1/transactions/commit"))
        .respond_with(ResponseTemplate::new(409).set_body_string("conflict"))
        .expect(1)
        .mount(&server)
        .await;

    let client = IcebergOutputClient::new(server.uri(), None, reqwest::Client::new());

    let err = client
        .commit(ICEBERG_DATASET, TXN_RID)
        .await
        .expect_err("409 must surface as OutputClientError");
    let msg = err.0;
    assert!(
        msg.contains("409"),
        "error message should mention the upstream status, got: {msg}"
    );
}

#[tokio::test]
async fn abort_is_a_noop_and_does_not_call_catalog() {
    let server = MockServer::start().await;

    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(500))
        .expect(0)
        .mount(&server)
        .await;

    let client = IcebergOutputClient::new(server.uri(), None, reqwest::Client::new());

    client
        .abort(ICEBERG_DATASET, TXN_RID)
        .await
        .expect("abort always succeeds");
    client
        .abort("ri.foundry.main.dataset.legacy", TXN_RID)
        .await
        .expect("abort always succeeds (non-iceberg)");
}
