//! P6 — `compute_health` flags schema drift when the latest two
//! `dataset_view_schemas.content_hash` values differ.
//!
//! Foundry doc § "Schema": schemas are metadata on a *dataset view*,
//! so each commit can land a different schema row. Drift is the
//! signal the QualityDashboard surfaces in its "Schema drift" card.
//! Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use chrono::Utc;
use serde_json::Value;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn schema_drift_flag_set_when_view_schemas_differ() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.drift";
    let dataset_id = common::seed_dataset_with_committed_at(&h.pool, rid, Utc::now()).await;
    common::seed_schema_drift(&h.pool, dataset_id).await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/health"))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    assert_eq!(
        body["schema_drift_flag"], true,
        "two view-schemas with different content_hash should trip drift: {body}"
    );
    // Col count derives from the latest view (which has 2 fields).
    assert_eq!(body["col_count"], 2);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn no_drift_when_only_one_view_schema_exists() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.no-drift";
    common::seed_dataset_with_committed_at(&h.pool, rid, Utc::now()).await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/health"))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    assert_eq!(
        body["schema_drift_flag"], false,
        "single view → no drift: {body}"
    );
}
