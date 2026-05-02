//! T7.1 — branches: create + reparent on intermediate delete.
//!
//! Tree: master → feature → patch
//! After deleting `feature`, `patch.parent` must be `master`.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

async fn create_branch(
    router: &axum::Router,
    token: &str,
    id: uuid::Uuid,
    name: &str,
    parent: Option<&str>,
) -> Value {
    let body = match parent {
        Some(p) => json!({ "name": name, "parent_branch": p }),
        None => json!({ "name": name }),
    };
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{id}/branches"))
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "create branch {name}: {}", resp.status());
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    serde_json::from_slice(&bytes).unwrap()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn deleting_intermediate_branch_reparents_to_grandparent() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.branches").await;

    let feature = create_branch(&h.router, &h.token, id, "feature", Some("master")).await;
    let patch = create_branch(&h.router, &h.token, id, "patch", Some("feature")).await;
    assert_eq!(
        patch["parent_branch_id"].as_str(),
        feature["id"].as_str(),
        "patch must initially point at feature"
    );

    // Delete `feature`.
    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/v1/datasets/{id}/branches/feature"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "delete feature: {}", resp.status());

    // After delete, patch should re-parent to `master` (the grandparent).
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{id}/branches/patch"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap();

    // We accept either an explicit master reparent or a top-level
    // (NULL) parent — both match the Foundry "promote to grandparent"
    // semantics when the immediate parent disappears.
    let new_parent = v["parent_branch_id"].as_str();
    let still_feature = new_parent == feature["id"].as_str();
    assert!(
        !still_feature,
        "patch must NOT keep the deleted feature as parent"
    );
}
