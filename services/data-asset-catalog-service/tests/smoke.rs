//! T7.3 — service smoke test.
//!
//! 1. Boots the catalog binary's router via [`build_router`] (the
//!    binary entry point in `src/main.rs` is just config + listener
//!    glue; the router is the contract).
//! 2. Asserts `GET /healthz` returns `200`.
//! 3. Asserts `GET /metrics` returns `200` and contains a metric with
//!    the `dataset_` prefix (registered eagerly in
//!    `src/metrics.rs::init`).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn healthz_returns_200_and_metrics_exposes_dataset_prefix() {
    let h = common::spawn().await;

    // /healthz
    let req = Request::builder()
        .method("GET")
        .uri("/healthz")
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);

    // /metrics — must contain at least one Prometheus metric line whose
    // name starts with `dataset_` (registered by `metrics::init`).
    let req = Request::builder()
        .method("GET")
        .uri("/metrics")
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body = String::from_utf8(bytes.to_vec()).unwrap();
    assert!(
        body.lines()
            .any(|l| { !l.starts_with('#') && l.starts_with("dataset_") }),
        "expected a `dataset_*` metric line in /metrics, got:\n{body}"
    );
}
