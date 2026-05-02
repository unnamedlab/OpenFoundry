//! T7.1 — transaction lifecycle.
//!
//! Covers:
//! * open → commit happy path.
//! * open → abort happy path.
//! * two concurrent OPEN transactions on the same branch are
//!   rejected (Foundry invariant: one open tx per branch).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

async fn post_json(router: &axum::Router, token: &str, uri: &str, body: Value) -> (StatusCode, Value) {
    let req = Request::builder()
        .method("POST")
        .uri(uri)
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("router");
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    (status, v)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn open_then_commit_succeeds() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.lifecycle-1").await;

    let (status, body) = post_json(
        &h.router,
        &h.token,
        &format!("/v1/datasets/{id}/branches/master/transactions"),
        json!({ "type": "SNAPSHOT", "providence": {}, "summary": "first" }),
    )
    .await;
    assert!(
        status.is_success(),
        "open should succeed (got {status}): {body}"
    );
    let txn_id = body["id"].as_str().expect("txn id in body").to_string();

    let (commit_status, _) = post_json(
        &h.router,
        &h.token,
        &format!("/v1/datasets/{id}/branches/master/transactions/{txn_id}:commit"),
        json!({}),
    )
    .await;
    assert!(commit_status.is_success(), "commit failed: {commit_status}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn open_then_abort_succeeds() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.lifecycle-2").await;

    let (status, body) = post_json(
        &h.router,
        &h.token,
        &format!("/v1/datasets/{id}/branches/master/transactions"),
        json!({ "type": "APPEND", "providence": {} }),
    )
    .await;
    assert!(status.is_success(), "open: {status} {body}");
    let txn_id = body["id"].as_str().unwrap().to_string();

    let (abort_status, _) = post_json(
        &h.router,
        &h.token,
        &format!("/v1/datasets/{id}/branches/master/transactions/{txn_id}:abort"),
        json!({}),
    )
    .await;
    assert!(abort_status.is_success(), "abort failed: {abort_status}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn two_concurrent_open_transactions_are_rejected() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.lifecycle-3").await;

    // First OPEN succeeds.
    let (s1, _) = post_json(
        &h.router,
        &h.token,
        &format!("/v1/datasets/{id}/branches/master/transactions"),
        json!({ "type": "SNAPSHOT", "providence": {} }),
    )
    .await;
    assert!(s1.is_success(), "first open should succeed: {s1}");

    // Second OPEN on same branch must be refused.
    let (s2, body2) = post_json(
        &h.router,
        &h.token,
        &format!("/v1/datasets/{id}/branches/master/transactions"),
        json!({ "type": "APPEND", "providence": {} }),
    )
    .await;
    assert!(
        !s2.is_success(),
        "second concurrent open MUST be rejected (got {s2}): {body2}"
    );
}
