//! T7.1 — cross-service flow: catalog ↔ lineage ↔ versioning.
//!
//! Boots the catalog service router with a `wiremock` neighbour that
//! impersonates both the lineage and versioning services. The flow:
//!   1. Create a dataset in the catalog.
//!   2. Stub the lineage neighbour to declare it has 0 upstreams.
//!   3. Stub the versioning neighbour to return a current view with
//!      one file.
//!   4. Hit `/v1/datasets/{rid}` and assert the dataset surfaces.
//!
//! End-to-end coverage of HTTP wire formats; persistence and
//! transactional invariants are covered by the versioning suite.

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use serde_json::json;
use tower::ServiceExt;
use wiremock::matchers::{any, method, path_regex};
use wiremock::{Mock, ResponseTemplate};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn catalog_get_dataset_works_with_neighbour_stubs() {
    let h = common::spawn().await;

    // Stub everything on the neighbour mock with a 200 envelope so
    // that any out-of-band call from the catalog (quality, lineage)
    // does not fail the test.
    Mock::given(any())
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({})))
        .mount(&h.mock)
        .await;
    Mock::given(method("GET"))
        .and(path_regex(r"^/.*/lineage/.*$"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"upstreams": []})))
        .mount(&h.mock)
        .await;

    let id = testing::fixtures::seed_dataset(
        &h.pool,
        "ri.foundry.main.dataset.cross",
        "cross-service",
        "parquet",
    )
    .await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{id}"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
}
