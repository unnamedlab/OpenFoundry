//! `POST .../alter-schema` is the explicit evolution path. It bumps
//! `schema-id` by one and persists the new schema; subsequent loads
//! reflect the change.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn alter_schema_adds_column_and_bumps_schema_id() {
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
                    json!({"namespace": ["alter"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/alter/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "evolving",
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

    // ALTER TABLE — add column `email`.
    let alter = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/alter/tables/evolving/alter-schema")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "updates": [
                            { "action": "add-column", "name": "email", "required": true, "type": "string" }
                        ]
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(alter.status(), StatusCode::OK);
    let alter_body: Value =
        serde_json::from_slice(&to_bytes(alter.into_body(), 1 << 20).await.unwrap()).unwrap();
    assert_eq!(alter_body["schema_id"], 1);
    let fields = alter_body["schema"]["fields"].as_array().unwrap();
    assert_eq!(fields.len(), 2);
    assert!(fields.iter().any(|f| f["name"] == "email"));

    // Reload — schema reflects the change.
    let load = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/namespaces/alter/tables/evolving")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    let body: Value =
        serde_json::from_slice(&to_bytes(load.into_body(), 1 << 20).await.unwrap()).unwrap();
    let schemas = body["metadata"]["schemas"].as_array().unwrap();
    assert!(schemas.iter().any(|s| {
        s["fields"]
            .as_array()
            .map(|f| f.iter().any(|f| f["name"] == "email"))
            .unwrap_or(false)
    }));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn alter_schema_drop_column_persists() {
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
                    json!({"namespace": ["dropcol"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/dropcol/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "users",
                        "schema": {
                            "schema-id": 0,
                            "type": "struct",
                            "fields": [
                                { "id": 1, "name": "id", "required": true, "type": "long" },
                                { "id": 2, "name": "obsolete", "required": false, "type": "string" }
                            ]
                        }
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    let alter = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/namespaces/dropcol/tables/users/alter-schema")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "updates": [{ "action": "drop-column", "name": "obsolete" }]
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(alter.status(), StatusCode::OK);
    let body: Value =
        serde_json::from_slice(&to_bytes(alter.into_body(), 1 << 20).await.unwrap()).unwrap();
    let fields = body["schema"]["fields"].as_array().unwrap();
    assert!(!fields.iter().any(|f| f["name"] == "obsolete"));
}
