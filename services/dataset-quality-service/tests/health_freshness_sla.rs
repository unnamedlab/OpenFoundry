//! P6 — `GET /v1/datasets/{rid}/health` reports freshness in seconds.
//!
//! Foundry doc § "Health checks" requires the freshness card to count
//! seconds since the most recent committed transaction. We seed a
//! dataset with a known `committed_at`, hit the endpoint, and assert
//! the freshness lands within a sane window of "now - committed_at".
//! Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use chrono::{Duration as ChronoDuration, Utc};
use serde_json::Value;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn health_endpoint_reports_freshness_against_last_commit() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.health-freshness";
    let committed_at = Utc::now() - ChronoDuration::hours(2);
    common::seed_dataset_with_committed_at(&h.pool, rid, committed_at).await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/health"))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::OK, "200 OK");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    assert_eq!(body["dataset_rid"], rid);
    assert_eq!(body["last_build_status"], "success");
    let freshness = body["freshness_seconds"].as_i64().expect("i64 freshness");
    let expected = (Utc::now() - committed_at).num_seconds();
    assert!(
        (freshness - expected).abs() < 30,
        "freshness drift > 30s: got {freshness}, expected ~{expected}; body: {body}"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn health_endpoint_404s_unknown_dataset() {
    let h = common::spawn().await;
    let req = Request::builder()
        .method("GET")
        .uri("/v1/datasets/ri.unknown.dataset/health")
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::NOT_FOUND);
}
