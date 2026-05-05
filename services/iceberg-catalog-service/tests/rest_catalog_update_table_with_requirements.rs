//! UpdateTable (POST namespaces/{ns}/tables/{tbl}) honours the
//! `requirements` block. `assert-uuid` with a wrong uuid must 409.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn update_table_rejects_when_assert_uuid_fails() {
    let h = common::spawn().await;
    let token = h.write_token();

    let _ = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({"namespace": ["mart"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    let create = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/mart/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "orders",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    let create_body: Value = serde_json::from_slice(
        &to_bytes(create.into_body(), 1 << 20).await.unwrap(),
    )
    .unwrap();
    let real_uuid = create_body["metadata"]["table-uuid"].as_str().unwrap();
    let wrong_uuid = "00000000-0000-0000-0000-000000000000";
    assert_ne!(real_uuid, wrong_uuid);

    // Send a CommitTable with assert-uuid pointing at the WRONG uuid.
    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/mart/tables/orders")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "requirements": [
                            {"type": "assert-uuid", "uuid": wrong_uuid}
                        ],
                        "updates": []
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::CONFLICT);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn update_table_set_properties_persists_changes() {
    let h = common::spawn().await;
    let token = h.write_token();

    let _ = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({"namespace": ["props"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    let create = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/props/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "t1",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(create.status(), StatusCode::OK);

    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/props/tables/t1")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "requirements": [],
                        "updates": [
                            {"action": "set-properties", "updates": {"foo": "bar"}}
                        ]
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::OK);
    let body: Value = serde_json::from_slice(
        &to_bytes(response.into_body(), 1 << 20).await.unwrap(),
    )
    .unwrap();
    assert_eq!(body["metadata"]["properties"]["foo"], json!("bar"));
}
