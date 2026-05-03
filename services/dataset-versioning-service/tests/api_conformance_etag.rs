//! P6 — Application reference: ETag + If-None-Match on resource GETs.
//!
//! Verifies that `GET /v1/datasets/{rid}/branches/{branch}` sets an
//! `ETag` header and that a follow-up request with `If-None-Match`
//! returns `304 Not Modified` with the same ETag and an empty body.
//! Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode, header};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn get_branch_returns_etag_then_304_on_match() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.etag";
    common::seed_dataset_with_master(&h.pool, rid).await;

    // First GET — collect ETag.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/branches/master"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK, "first GET branch");
    let etag = resp
        .headers()
        .get(header::ETAG)
        .expect("ETag header must be present on resource GET")
        .to_str()
        .expect("etag is ascii")
        .to_string();
    assert!(
        etag.starts_with('"') && etag.ends_with('"'),
        "ETag must be quoted per RFC 7232: {etag}"
    );
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    assert!(!bytes.is_empty(), "200 path returns body");

    // Second GET with If-None-Match → 304 + empty body, ETag echoed back.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/branches/master"))
        .header("authorization", format!("Bearer {}", h.token))
        .header(header::IF_NONE_MATCH, etag.clone())
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::NOT_MODIFIED);
    let echoed = resp
        .headers()
        .get(header::ETAG)
        .map(|v| v.to_str().unwrap().to_string());
    assert_eq!(echoed.as_deref(), Some(etag.as_str()));
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    assert!(bytes.is_empty(), "304 must have empty body");

    // Third GET with a non-matching If-None-Match → 200 with body.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/branches/master"))
        .header("authorization", format!("Bearer {}", h.token))
        .header(header::IF_NONE_MATCH, "\"deadbeef\"")
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK, "non-match ⇒ 200");
}
