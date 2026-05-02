//! T7.1 — branch fallback resolution.
//!
//! Foundry doc example: a `feature` branch with fallback to `master`
//! sees `master`'s datasets when nothing has been written on `feature`.
//! This test asserts the `PUT /fallbacks` + `GET /fallbacks` round-trip
//! and that the fallback chain is reflected back to the caller.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn put_then_get_fallbacks_round_trip() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.fallbacks").await;

    // Create `feature`.
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{id}/branches"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({
                "name": "feature",
                "parent_branch": "master",
            }))
            .unwrap(),
        ))
        .unwrap();
    assert!(
        h.router
            .clone()
            .oneshot(req)
            .await
            .unwrap()
            .status()
            .is_success()
    );

    // Configure fallback chain: feature → master.
    let req = Request::builder()
        .method("PUT")
        .uri(format!("/v1/datasets/{id}/branches/feature/fallbacks"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({
                "fallbacks": ["master"],
            }))
            .unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(
        resp.status().is_success() || resp.status() == StatusCode::CREATED,
        "put fallbacks: {}",
        resp.status()
    );

    // Read it back.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{id}/branches/feature/fallbacks"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap();
    let chain = v["fallbacks"].as_array().cloned().unwrap_or_default();
    assert!(
        chain.iter().any(|c| c.as_str() == Some("master")),
        "fallback chain must include master, got {v}"
    );
}
