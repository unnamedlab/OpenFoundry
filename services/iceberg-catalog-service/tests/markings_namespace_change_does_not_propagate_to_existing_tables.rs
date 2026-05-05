//! Snapshot semantics — a namespace marking added *after* a table
//! exists does not retroactively appear on the table's effective
//! markings. The table's inherited set was stamped at creation time
//! and only `manage_markings` (table-level) can mutate it.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn later_namespace_marking_change_does_not_retro_propagate() {
    let h = common::spawn().await;
    let token = h.write_token();

    // Create namespace + table BEFORE marking the namespace.
    for body in [
        json!({"namespace": ["unsecured"], "properties": {}}),
    ] {
        h.router
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/iceberg/v1/namespaces")
                    .header("content-type", "application/json")
                    .header("authorization", format!("Bearer {token}"))
                    .body(Body::from(body.to_string()))
                    .unwrap(),
            )
            .await
            .unwrap();
    }
    h.router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/unsecured/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "early",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    // Now add a marking to the namespace.
    h.router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/unsecured/markings")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(json!({"markings": ["restricted"]}).to_string()))
                .unwrap(),
        )
        .await
        .unwrap();

    // Re-read the table's markings — must NOT contain `restricted`.
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces/unsecured/tables/early/markings")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let body: Value =
        serde_json::from_slice(&to_bytes(resp.into_body(), 1 << 20).await.unwrap()).unwrap();
    let effective = body["effective"].as_array().cloned().unwrap_or_default();
    assert!(
        !effective.iter().any(|m| m["name"] == "restricted"),
        "later namespace marking unexpectedly propagated: {body}"
    );
}
