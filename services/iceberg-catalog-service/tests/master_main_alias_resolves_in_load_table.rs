//! `LoadTable` accepting `?ref=master` resolves transparently to
//! `main` and emits the `X-Foundry-Branch-Alias: master->main`
//! response header.

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use serde_json::json;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn load_table_ref_master_emits_alias_header() {
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
                    json!({"namespace": ["alias"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/alias/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "master_table",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
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
                .uri("/iceberg/v1/namespaces/alias/tables/master_table?ref=master")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::OK);

    let header = response
        .headers()
        .get("x-foundry-branch-alias")
        .expect("alias header must be set when ref=master");
    assert_eq!(header.to_str().unwrap(), "master->main");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn load_table_without_ref_does_not_emit_alias_header() {
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
                    json!({"namespace": ["plain"], "properties": {}}).to_string(),
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
                .uri("/iceberg/v1/namespaces/plain/tables")
                .header("content-type", "application/json")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::from(
                    json!({
                        "name": "tbl",
                        "schema": {"schema-id": 0, "type": "struct", "fields": []}
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
                .uri("/iceberg/v1/namespaces/plain/tables/tbl")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert!(response.headers().get("x-foundry-branch-alias").is_none());
}
