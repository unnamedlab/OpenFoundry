//! T7.1 — concurrent transactions: 50 branches × 1 APPEND each, no
//! corruption. Runs on the multi-thread tokio runtime.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use serde_json::{Value, json};
use sqlx::Row;
use tower::ServiceExt;

const BRANCHES: usize = 50;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn fifty_concurrent_branch_appends_complete_without_corruption() {
    let h = common::spawn().await;
    let id = common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.concurrent").await;

    // Pre-create the 50 branches sequentially (branch creation is cheap
    // and serial here keeps the test deterministic; the parallelism
    // we care about is the transaction storm).
    for i in 0..BRANCHES {
        let req = Request::builder()
            .method("POST")
            .uri(format!("/v1/datasets/{id}/branches"))
            .header("authorization", format!("Bearer {}", h.token))
            .header("content-type", "application/json")
            .body(Body::from(serde_json::to_vec(&json!({
                "name": format!("br-{i}"),
                "parent_branch": "master",
            })).unwrap()))
            .unwrap();
        let resp = h.router.clone().oneshot(req).await.expect("router");
        assert!(resp.status().is_success(), "create br-{i}");
    }

    // Fire 50 concurrent open+commit cycles, one per branch.
    let mut handles = Vec::new();
    for i in 0..BRANCHES {
        let router = h.router.clone();
        let token = h.token.clone();
        handles.push(tokio::spawn(async move {
            let branch = format!("br-{i}");
            let req = Request::builder()
                .method("POST")
                .uri(format!("/v1/datasets/{id}/branches/{branch}/transactions"))
                .header("authorization", format!("Bearer {token}"))
                .header("content-type", "application/json")
                .body(Body::from(b"{\"type\":\"APPEND\",\"providence\":{}}".to_vec()))
                .unwrap();
            let resp = router.clone().oneshot(req).await.expect("router");
            assert!(resp.status().is_success(), "open on {branch}");
            let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
            let v: Value = serde_json::from_slice(&bytes).unwrap();
            let txn = v["id"].as_str().unwrap().to_string();
            let req = Request::builder()
                .method("POST")
                .uri(format!(
                    "/v1/datasets/{id}/branches/{branch}/transactions/{txn}:commit"
                ))
                .header("authorization", format!("Bearer {token}"))
                .header("content-type", "application/json")
                .body(Body::from(b"{}".to_vec()))
                .unwrap();
            let resp = router.oneshot(req).await.expect("router");
            assert!(resp.status().is_success(), "commit on {branch}");
        }));
    }
    for h in handles {
        h.await.expect("task");
    }

    // Invariant: exactly 50 COMMITTED transactions for this dataset
    // (plus zero ABORTED).
    let row = sqlx::query(
        "SELECT
           COUNT(*) FILTER (WHERE status = 'COMMITTED') AS committed,
           COUNT(*) FILTER (WHERE status = 'ABORTED')   AS aborted,
           COUNT(*) FILTER (WHERE status = 'OPEN')      AS still_open
         FROM dataset_transactions WHERE dataset_id = $1",
    )
    .bind(id)
    .fetch_one(&h.pool)
    .await
    .expect("counts");
    let committed: i64 = row.get("committed");
    let aborted: i64 = row.get("aborted");
    let still_open: i64 = row.get("still_open");
    assert_eq!(committed as usize, BRANCHES);
    assert_eq!(aborted, 0);
    assert_eq!(still_open, 0);
}
