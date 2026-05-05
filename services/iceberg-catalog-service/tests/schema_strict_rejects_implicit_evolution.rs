//! A CommitTable that includes an `add-schema` whose schema diverges
//! from the current one (without a prior alter-schema call) is
//! rejected with 422 SCHEMA_INCOMPATIBLE_REQUIRES_ALTER.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn add_schema_with_extra_column_is_rejected_422() {
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
                    json!({"namespace": ["strict"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    let _ = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/strict/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "users",
                        "schema": {
                            "schema-id": 0,
                            "type": "struct",
                            "fields": [
                                { "id": 1, "name": "id", "required": true, "type": "long" }
                            ]
                        }
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/strict/tables/users")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "requirements": [],
                        "updates": [
                            {
                                "action": "add-schema",
                                "schema": {
                                    "schema-id": 1,
                                    "type": "struct",
                                    "fields": [
                                        { "id": 1, "name": "id", "required": true, "type": "long" },
                                        { "id": 2, "name": "email", "required": true, "type": "string" }
                                    ]
                                }
                            }
                        ]
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    let body: Value =
        serde_json::from_slice(&to_bytes(response.into_body(), 1 << 20).await.unwrap()).unwrap();
    assert_eq!(
        body["error"]["type"],
        json!("SCHEMA_INCOMPATIBLE_REQUIRES_ALTER")
    );
    let deltas = body["error"]["diff"]["deltas"].as_array().unwrap();
    assert!(deltas.iter().any(|d| d["kind"] == "added-column" && d["name"] == "email"));
}
