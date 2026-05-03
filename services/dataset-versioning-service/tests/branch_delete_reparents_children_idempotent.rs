//! P1 — repeated deletion of an already-soft-deleted branch is a
//! no-op and must keep the children's ancestry intact.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn req(
    router: &axum::Router,
    token: &str,
    method: &str,
    uri: &str,
    body: Option<Value>,
) -> (StatusCode, Value) {
    let mut builder = Request::builder()
        .method(method)
        .uri(uri)
        .header("authorization", format!("Bearer {token}"));
    let body_bytes = match body {
        Some(value) => {
            builder = builder.header("content-type", "application/json");
            Body::from(serde_json::to_vec(&value).unwrap())
        }
        None => Body::empty(),
    };
    let resp = router
        .clone()
        .oneshot(builder.body(body_bytes).unwrap())
        .await
        .unwrap();
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let json = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    (status, json)
}

async fn create_child(
    router: &axum::Router,
    token: &str,
    dataset_id: Uuid,
    name: &str,
    parent: &str,
) -> Value {
    let (status, body) = req(
        router,
        token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({ "name": name, "source": { "from_branch": parent } })),
    )
    .await;
    assert!(status.is_success(), "create {name}: {status} {body}");
    body
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn delete_branch_three_times_keeps_ancestry_intact() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.delete-idempotent",
    )
    .await;

    let _b = create_child(&h.router, &h.token, dataset_id, "B", "master").await;
    let _c1 = create_child(&h.router, &h.token, dataset_id, "C1", "B").await;
    let _c2 = create_child(&h.router, &h.token, dataset_id, "C2", "B").await;

    // First delete: 200 with two reparented children.
    let (status, body) = req(
        &h.router,
        &h.token,
        "DELETE",
        &format!("/v1/datasets/{dataset_id}/branches/B"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK, "first delete: {status} {body}");
    let reparented = body["reparented"].as_array().cloned().unwrap_or_default();
    assert_eq!(reparented.len(), 2, "expected two children reparented");

    // Second & third delete: branch is now gone so it 404s.
    for attempt in 0..2 {
        let (s, b) = req(
            &h.router,
            &h.token,
            "DELETE",
            &format!("/v1/datasets/{dataset_id}/branches/B"),
            None,
        )
        .await;
        assert_eq!(
            s,
            StatusCode::NOT_FOUND,
            "delete #{} after soft-delete must 404, got {s} {b}",
            attempt + 2
        );
    }

    // Children still exist and now point at master (the grandparent).
    for child_name in ["C1", "C2"] {
        let (s, b) = req(
            &h.router,
            &h.token,
            "GET",
            &format!("/v1/datasets/{dataset_id}/branches/{child_name}"),
            None,
        )
        .await;
        assert_eq!(s, StatusCode::OK, "child {child_name}: {s} {b}");
        let parent_id = b["parent_branch_id"].as_str();
        assert!(parent_id.is_some(), "child {child_name} must keep a parent (=master)");
    }
}
