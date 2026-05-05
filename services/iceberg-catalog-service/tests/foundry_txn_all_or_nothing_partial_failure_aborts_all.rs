//! Replicates the doc img_001 example: `orders/customers` joined to
//! `order_summary/customer_metrics`. The commit submits both writes
//! together — when one of them violates an `assert-ref-snapshot-id`
//! requirement, the whole batched commit must roll back so neither
//! table reflects the partial run.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn partial_failure_aborts_every_table_in_the_batch() {
    let h = common::spawn().await;
    let token = h.write_token();

    // Set up: namespace `pipe`, two tables: order_summary + customer_metrics.
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
                    json!({"namespace": ["pipe"], "properties": {}}).to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    for name in ["order_summary", "customer_metrics"] {
        let _ = h
            .router
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/iceberg/v1/namespaces/pipe/tables")
                    .header("content-type", "application/json")
                    .header("authorization", format!("Bearer {token}"))
                    .body(Body::from(
                        json!({
                            "name": name,
                            "schema": {"schema-id": 0, "type": "struct", "fields": []}
                        })
                        .to_string(),
                    ))
                    .unwrap(),
            )
            .await
            .unwrap();
    }

    // Get the freshly-created tables' uuids.
    let summary_uuid = fetch_table_uuid(&h, &token, "pipe", "order_summary").await;
    let metrics_uuid = fetch_table_uuid(&h, &token, "pipe", "customer_metrics").await;

    // Multi-table commit: the first table change passes
    // (assert-uuid matches), the second fails (assert-uuid wrong).
    // Per the doc, both must be reverted.
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
                        "build_rid": "ri.foundry.main.build.test-1",
                        "table-changes": [
                            {
                                "identifier": {"namespace": ["pipe"], "name": "order_summary"},
                                "requirements": [{"type": "assert-uuid", "uuid": summary_uuid}],
                                "updates": [{"action": "set-properties", "updates": {"foo": "bar"}}]
                            },
                            {
                                "identifier": {"namespace": ["pipe"], "name": "customer_metrics"},
                                "requirements": [{"type": "assert-uuid", "uuid": "00000000-0000-0000-0000-000000000000"}],
                                "updates": [{"action": "set-properties", "updates": {"foo": "bar"}}]
                            }
                        ]
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::CONFLICT);

    // Now LOAD both tables and confirm neither reflects the partial
    // change (`foo: bar`). All-or-nothing.
    for name in ["order_summary", "customer_metrics"] {
        let load = h
            .router
            .clone()
            .oneshot(
                Request::builder()
                    .uri(format!("/iceberg/v1/namespaces/pipe/tables/{name}"))
                    .header("authorization", format!("Bearer {token}"))
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(load.status(), StatusCode::OK);
        let body: Value =
            serde_json::from_slice(&to_bytes(load.into_body(), 1 << 20).await.unwrap()).unwrap();
        let props = &body["metadata"]["properties"];
        assert!(
            !props.get("foo").is_some(),
            "table {name} must NOT contain partially-committed property"
        );
    }

    // Sanity: the rejected uuid was the customer_metrics one.
    let _ = metrics_uuid;
}

async fn fetch_table_uuid(
    h: &common::Harness,
    token: &str,
    namespace: &str,
    name: &str,
) -> String {
    let load = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri(format!("/iceberg/v1/namespaces/{namespace}/tables/{name}"))
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    let body: Value =
        serde_json::from_slice(&to_bytes(load.into_body(), 1 << 20).await.unwrap()).unwrap();
    body["metadata"]["table-uuid"].as_str().unwrap().to_string()
}
