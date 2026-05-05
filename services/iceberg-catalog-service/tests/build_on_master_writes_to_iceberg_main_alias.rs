//! When a Foundry build runs on `master` and the target dataset is
//! Iceberg-backed, the catalog rewrites `master` to `main` per the
//! doc § "Default branches". This test sends a multi-table commit
//! whose `assert-ref-snapshot-id` requirement uses `ref=master` and
//! verifies the catalog accepts it.

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use serde_json::json;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn multi_table_commit_with_ref_master_is_accepted() {
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
                    json!({"namespace": ["build_alias"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/build_alias/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "events",
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

    // Multi-table commit using ref=master. The catalog stores no
    // refs row (the table was just created), so assert-ref-snapshot-id
    // expects null. The crucial part of this test is that ref=master
    // does NOT cause a 400 — the alias is resolved transparently.
    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/transactions/commit")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "build_rid": "ri.foundry.main.build.master-alias",
                        "table-changes": [
                            {
                                "identifier": {"namespace": ["build_alias"], "name": "events"},
                                "requirements": [
                                    {"type": "assert-ref-snapshot-id", "ref": "master", "snapshot-id": null}
                                ],
                                "updates": [
                                    {"action": "set-properties", "updates": {"foundry.branch": "master"}}
                                ]
                            }
                        ]
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();

    // 200 OK — alias accepted.
    assert_eq!(response.status(), StatusCode::OK);
}
