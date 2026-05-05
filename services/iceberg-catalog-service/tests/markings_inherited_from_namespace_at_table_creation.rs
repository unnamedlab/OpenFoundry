//! Per Foundry doc snapshot semantics: the table created inside a
//! namespace marked `pii` inherits `pii` as its effective marking.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn table_inherits_namespace_markings_at_creation() {
    let h = common::spawn().await;
    let token = h.write_token();

    // Create namespace then mark it pii.
    h.router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({"namespace": ["secured"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    let mark = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/secured/markings")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(json!({"markings": ["pii"]}).to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(mark.status(), StatusCode::OK);

    // Create table in the marked namespace.
    let create = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/secured/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "events",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(create.status(), StatusCode::OK);

    // Read table markings — `pii` should appear in `inherited_from_namespace`.
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces/secured/tables/events/markings")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let body: Value =
        serde_json::from_slice(&to_bytes(resp.into_body(), 1 << 20).await.unwrap()).unwrap();
    let inherited = body["inherited_from_namespace"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    assert!(
        inherited.iter().any(|m| m["name"] == "pii"),
        "table missing inherited pii marking, got {body}"
    );
}
