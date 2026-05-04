//! P1 — `source.from_transaction_rid` rejects OPEN transactions.

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
async fn rejects_branch_creation_from_open_transaction() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.from-open-tx").await;

    let (open_status, open_body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/transactions"),
        Some(json!({ "type": "SNAPSHOT", "providence": {} })),
    )
    .await;
    assert!(open_status.is_success(), "open: {open_status} {open_body}");
    let open_txn_id = Uuid::parse_str(open_body["id"].as_str().unwrap()).unwrap();

    let (status, body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({
            "name": "feature",
            "source": {
                "from_transaction_rid": format!("ri.foundry.main.transaction.{open_txn_id}")
            }
        })),
    )
    .await;
    assert_eq!(
        status,
        StatusCode::UNPROCESSABLE_ENTITY,
        "expected 422, got {status} {body}"
    );
    assert_eq!(body["status"], "OPEN");
}
