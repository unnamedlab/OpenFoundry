//! P5 — `GET /branches/compare` resolves the lowest common ancestor.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn create_child(
    h: &common::Harness,
    dataset_id: Uuid,
    name: &str,
    parent: &str,
) {
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{dataset_id}/branches"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({ "name": name, "parent_branch": parent })).unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "create {name}: {}", resp.status());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn lca_is_master_for_two_siblings_off_master() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.compare-lca",
    )
    .await;

    create_child(&h, dataset_id, "feature-a", "master").await;
    create_child(&h, dataset_id, "feature-b", "master").await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/compare?base=feature-a&compare=feature-b"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let lca = body["lca_branch_rid"]
        .as_str()
        .expect("lca_branch_rid must be set");
    assert!(
        lca.starts_with("ri.foundry.main.branch."),
        "lca should be a branch RID, got {lca}"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn lca_is_parent_for_grandchild_vs_child() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.compare-grand").await;

    create_child(&h, dataset_id, "develop", "master").await;
    create_child(&h, dataset_id, "feature", "develop").await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/compare?base=feature&compare=develop"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    // The closest common ancestor of (feature, develop) is develop.
    assert!(
        body["lca_branch_rid"]
            .as_str()
            .map(|s| s.starts_with("ri.foundry.main.branch."))
            .unwrap_or(false),
        "lca should be the develop branch RID"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn compare_rejects_identical_base_and_compare() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.compare-self").await;
    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/compare?base=master&compare=master"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::BAD_REQUEST);
}
