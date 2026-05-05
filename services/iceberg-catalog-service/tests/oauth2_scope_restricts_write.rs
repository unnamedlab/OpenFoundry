//! A token issued with only `api:iceberg-read` is forbidden from
//! mutating endpoints (POST/DELETE).

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use serde_json::json;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn read_scope_cannot_create_namespace() {
    let h = common::spawn().await;
    let token = h.read_token();

    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({"namespace": ["forbidden"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::FORBIDDEN);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn read_scope_can_get_config() {
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
}
