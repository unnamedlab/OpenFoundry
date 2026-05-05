//! `GET /iceberg/v1/config` returns the warehouse URI as a default.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::Value;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn config_endpoint_returns_warehouse_default() {
    let h = common::spawn().await;
    let token = h.read_token();

    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/config")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let bytes = to_bytes(response.into_body(), 1 << 20).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["defaults"]["warehouse"], "s3://foundry-iceberg-test");
    assert!(body["overrides"].is_object());
}
