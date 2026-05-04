//! P1 — Foundry guarantee "one open transaction per branch".
//!
//! While a branch carries an OPEN transaction, a second
//! `POST /transactions` returns 409 with `error =
//! BRANCH_HAS_OPEN_TRANSACTION` and `open_transaction_rid` set.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn req(
    router: &axum::Router,
    token: &str,
    method: &str,
    uri: &str,
    body: Option<Value>,
) -> (StatusCode, Value) {
    let mut builder = Request::builder()
        .method(method)
        .uri(uri)
        .header("authorization", format!("Bearer {token}"));
    let body_bytes = match body {
        Some(value) => {
            builder = builder.header("content-type", "application/json");
            Body::from(serde_json::to_vec(&value).unwrap())
        }
        None => Body::empty(),
    };
    let resp = router
        .clone()
        .oneshot(builder.body(body_bytes).unwrap())
        .await
        .unwrap();
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let json = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    (status, json)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn second_open_on_branch_with_open_tx_returns_conflict_with_rid() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.open-blocks-second")
            .await;

    let (first_status, first_body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/transactions"),
        Some(json!({ "type": "SNAPSHOT", "providence": {} })),
    )
    .await;
    assert!(
        first_status.is_success(),
        "first open: {first_status} {first_body}"
    );
    let first_txn_id = Uuid::parse_str(first_body["id"].as_str().unwrap()).unwrap();

    let (second_status, second_body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/transactions"),
        Some(json!({ "type": "APPEND", "providence": {} })),
    )
    .await;
    assert_eq!(
        second_status,
        StatusCode::CONFLICT,
        "expected 409, got {second_status} {second_body}"
    );
    assert_eq!(second_body["error"], "BRANCH_HAS_OPEN_TRANSACTION");
    assert_eq!(
        second_body["open_transaction_id"]
            .as_str()
            .and_then(|s| Uuid::parse_str(s).ok()),
        Some(first_txn_id),
        "open_transaction_id must point at the first OPEN tx"
    );
    assert_eq!(
        second_body["open_transaction_rid"],
        json!(format!("ri.foundry.main.transaction.{first_txn_id}"))
    );
}
