//! P6 — Application reference: 207 Multi-Status batch endpoints.
//!
//! Verifies `POST /v1/datasets/{rid}/transactions:batchGet` returns
//! status 207 with one BatchItemResult per input id, mixing 200 (hit),
//! 404 (unknown id), and 400 (malformed uuid) per-item statuses.
//! Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn batch_get_transactions_returns_207_with_per_item_status() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.batch";
    let dataset_id = common::seed_dataset_with_master(&h.pool, rid).await;
    let branch_id: uuid::Uuid = sqlx::query_scalar(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();

    // One real committed txn.
    let real_txn = uuid::Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at, committed_at)
           VALUES ($1, $2, $3, 'master', 'SNAPSHOT', 'COMMITTED',
                   'batch test', NOW(), NOW())"#,
    )
    .bind(real_txn)
    .bind(dataset_id)
    .bind(branch_id)
    .execute(&h.pool)
    .await
    .expect("seed real txn");

    let unknown_txn = uuid::Uuid::now_v7();
    let bad_id = "not-a-uuid";

    let body = json!({
        "ids": [real_txn.to_string(), unknown_txn.to_string(), bad_id]
    });

    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{rid}/transactions:batchGet"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(
        resp.status(),
        StatusCode::MULTI_STATUS,
        "batchGet must return 207 Multi-Status"
    );

    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let items: Value = serde_json::from_slice(&bytes).unwrap();
    let arr = items.as_array().expect("207 body must be an array");
    assert_eq!(arr.len(), 3);

    // Order is preserved: 200, 404, 400.
    assert_eq!(arr[0]["status"], 200);
    assert_eq!(arr[0]["id"], real_txn.to_string());
    assert_eq!(arr[0]["data"]["id"], real_txn.to_string());

    assert_eq!(arr[1]["status"], 404);
    assert_eq!(arr[1]["id"], unknown_txn.to_string());
    assert_eq!(arr[1]["error"]["code"], "TRANSACTION_NOT_FOUND");

    assert_eq!(arr[2]["status"], 400);
    assert_eq!(arr[2]["id"], bad_id);
    assert_eq!(arr[2]["error"]["code"], "TRANSACTION_BAD_ID");
}
