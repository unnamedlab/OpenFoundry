//! Namespace CRUD against `/iceberg/v1/namespaces*` endpoints.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn namespaces_can_be_created_listed_loaded_and_dropped() {
    let h = common::spawn().await;
    let token = h.write_token();

    // Create
    let create = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "namespace": ["analytics"],
                        "properties": { "owner": "alice" }
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(create.status(), StatusCode::OK, "create namespace");

    // List — should include analytics.
    let list = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(list.status(), StatusCode::OK, "list namespaces");
    let bytes = to_bytes(list.into_body(), 1 << 20).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert!(body["namespaces"]
        .as_array()
        .unwrap()
        .iter()
        .any(|ns| ns == &json!(["analytics"])));

    // Load
    let load = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces/analytics")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(load.status(), StatusCode::OK);

    // Drop
    let drop = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("DELETE")
                .uri("/iceberg/v1/namespaces/analytics")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(drop.status(), StatusCode::NO_CONTENT);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn duplicate_namespace_returns_409() {
    let h = common::spawn().await;
    let token = h.write_token();

    let make_request = || {
        Request::builder()
            .method("POST")
            .uri("/iceberg/v1/namespaces")
            .header("content-type", "application/json")
            .header("authorization", format!("Bearer {token}"))
            .body(Body::from(
                json!({"namespace": ["dup"], "properties": {}}).to_string(),
            ))
            .unwrap()
    };

    let first = h.router.clone().oneshot(make_request()).await.unwrap();
    assert_eq!(first.status(), StatusCode::OK);

    let second = h.router.clone().oneshot(make_request()).await.unwrap();
    assert_eq!(second.status(), StatusCode::CONFLICT);
}
