//! The `metadata.json` we produce for a freshly-created table conforms
//! to the structural rules of the Apache Iceberg v2 spec. We don't
//! pull the canonical Apache JSON Schema at test time (the spec is
//! published as Markdown, not as JSON Schema); instead we encode the
//! invariants the spec mandates.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn create_table_metadata_conforms_to_spec_v2() {
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
                    json!({"namespace": ["spec"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/spec/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "events",
                        "schema": {
                            "schema-id": 0,
                            "type": "struct",
                            "fields": [
                                {"id": 1, "name": "id", "required": true, "type": "long"}
                            ]
                        }
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    let body = to_bytes(create.into_body(), 1 << 20).await.unwrap();
    let payload: Value = serde_json::from_slice(&body).unwrap();
    let metadata = &payload["metadata"];

    // ── REQUIRED top-level keys per § Table metadata ────────────────────
    for key in [
        "format-version",
        "table-uuid",
        "location",
        "last-sequence-number",
        "last-updated-ms",
        "current-schema-id",
        "schemas",
        "default-spec-id",
        "partition-specs",
        "default-sort-order-id",
        "sort-orders",
        "properties",
        "current-snapshot-id",
        "refs",
        "snapshots",
        "snapshot-log",
        "metadata-log",
    ] {
        assert!(
            metadata.get(key).is_some(),
            "metadata.json missing required key `{key}`: {payload}",
        );
    }

    // ── Type-level invariants ───────────────────────────────────────────
    assert!(metadata["format-version"].as_i64().unwrap() == 2);
    assert!(metadata["schemas"].is_array());
    assert!(metadata["partition-specs"].is_array());
    assert!(metadata["sort-orders"].is_array());
    assert!(metadata["snapshots"].is_array());
    assert!(metadata["refs"].is_object());

    // ── table-uuid must be a parseable UUID (RFC 4122) ──────────────────
    let uuid = metadata["table-uuid"].as_str().unwrap();
    assert!(uuid::Uuid::parse_str(uuid).is_ok());

    // ── location must be non-empty and use a known scheme ───────────────
    let loc = metadata["location"].as_str().unwrap();
    assert!(!loc.is_empty());
    let scheme = loc.split("://").next().unwrap();
    assert!(
        ["s3", "file", "abfss", "gs", "memory"].contains(&scheme),
        "unexpected scheme in location: {loc}"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn metadata_location_path_follows_v_n_pattern() {
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
                    json!({"namespace": ["paths"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/paths/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "t",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    let payload: Value = serde_json::from_slice(
        &to_bytes(create.into_body(), 1 << 20).await.unwrap(),
    )
    .unwrap();
    let location = payload["metadata-location"].as_str().unwrap();
    let segments: Vec<_> = location.split('/').collect();
    let last = *segments.last().unwrap();
    assert!(last.starts_with("v") && last.ends_with(".metadata.json"));
}
