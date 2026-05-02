//! T7.1 — `views/at?ts=` reconstructs a historical view.
//!
//! Sequence: open+commit a SNAPSHOT, capture the timestamp, open+commit
//! a second SNAPSHOT, then `GET /views/at?ts=<first_commit_ts>` and
//! assert the head transaction equals the first commit (not the
//! second).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use chrono::{DateTime, Utc};
use serde_json::Value;
use tower::ServiceExt;

async fn open_commit(router: &axum::Router, token: &str, id: uuid::Uuid) -> (String, DateTime<Utc>) {
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{id}/branches/master/transactions"))
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .body(Body::from(b"{\"type\":\"SNAPSHOT\",\"providence\":{}}".to_vec()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap();
    let txn = v["id"].as_str().unwrap().to_string();
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{id}/branches/master/transactions/{txn}:commit"))
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .body(Body::from(b"{}".to_vec()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success());
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    let committed_at: DateTime<Utc> = v["committed_at"]
        .as_str()
        .and_then(|s| s.parse().ok())
        .unwrap_or_else(Utc::now);
    (txn, committed_at)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn view_at_timestamp_returns_state_at_first_commit() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.viewat").await;

    let (txn1, ts1) = open_commit(&h.router, &h.token, id).await;
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;
    let (txn2, _ts2) = open_commit(&h.router, &h.token, id).await;
    assert_ne!(txn1, txn2);

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{id}/views/at?ts={}&branch=master",
            ts1.to_rfc3339()
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 256 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    // The historical view at ts1 must point at txn1, not txn2.
    let head = v["head_transaction_id"].as_str().unwrap_or_default();
    assert_eq!(head, txn1, "view@ts1 should pin head to first commit");
}
