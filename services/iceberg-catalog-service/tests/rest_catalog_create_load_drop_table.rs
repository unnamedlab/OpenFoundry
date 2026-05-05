//! End-to-end create → load → drop on `/iceberg/v1/namespaces/{ns}/tables*`.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn create_load_drop_table_round_trip() {
    let h = common::spawn().await;
    let token = h.write_token();

    let create_ns = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({"namespace": ["events"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(create_ns.status(), StatusCode::OK);

    let create_tbl = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/events/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "logins",
                        "schema": {
                            "schema-id": 0,
                            "type": "struct",
                            "fields": [
                                {"id": 1, "name": "id", "required": true, "type": "long"},
                                {"id": 2, "name": "ts", "required": true, "type": "timestamptz"}
                            ]
                        },
                        "properties": {"format": "parquet"},
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(create_tbl.status(), StatusCode::OK);
    let body = to_bytes(create_tbl.into_body(), 1 << 20).await.unwrap();
    let payload: Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(payload["metadata"]["format-version"], json!(2));
    assert!(
        payload["metadata-location"]
            .as_str()
            .unwrap()
            .ends_with("/v1.metadata.json")
    );

    // Load
    let load = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces/events/tables/logins")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(load.status(), StatusCode::OK);

    // HEAD
    let head = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("HEAD")
                .uri("/iceberg/v1/namespaces/events/tables/logins")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(head.status(), StatusCode::NO_CONTENT);

    // Drop
    let drop = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("DELETE")
                .uri("/iceberg/v1/namespaces/events/tables/logins")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(drop.status(), StatusCode::NO_CONTENT);

    // Loading again must 404.
    let after = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces/events/tables/logins")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(after.status(), StatusCode::NOT_FOUND);
}
