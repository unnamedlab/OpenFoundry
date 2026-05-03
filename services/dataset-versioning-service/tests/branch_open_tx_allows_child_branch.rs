//! P1 — Foundry doc explicitly allows creating a child branch off a
//! parent that has an OPEN transaction. The child must point at the
//! parent's last *committed* HEAD, not at the OPEN one.

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
async fn child_branch_can_be_created_while_parent_has_open_tx() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.open-tx-allows-child",
    )
    .await;

    // 1) Commit a SNAPSHOT so master has a HEAD pointer.
    let (open_status, open_body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/transactions"),
        Some(json!({ "type": "SNAPSHOT", "providence": {} })),
    )
    .await;
    assert!(open_status.is_success(), "open: {open_status} {open_body}");
    let committed_txn_id = Uuid::parse_str(open_body["id"].as_str().unwrap()).unwrap();
    let (commit_status, _) = req(
        &h.router,
        &h.token,
        "POST",
        &format!(
            "/v1/datasets/{dataset_id}/branches/master/transactions/{committed_txn_id}:commit"
        ),
        Some(json!({})),
    )
    .await;
    assert!(commit_status.is_success(), "commit: {commit_status}");

    // 2) Open a second tx and leave it OPEN.
    let (second_status, second_body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/transactions"),
        Some(json!({ "type": "APPEND", "providence": {} })),
    )
    .await;
    assert!(second_status.is_success(), "second open: {second_status} {second_body}");
    let open_txn_id = Uuid::parse_str(second_body["id"].as_str().unwrap()).unwrap();

    // 3) Creating a child branch from master must succeed and the
    //    child's HEAD must equal the COMMITTED tx, not the OPEN one.
    let (status, body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({ "name": "feature", "source": { "from_branch": "master" } })),
    )
    .await;
    assert_eq!(status, StatusCode::CREATED, "create: {status} {body}");
    let child_head = body["head_transaction_id"]
        .as_str()
        .and_then(|s| Uuid::parse_str(s).ok());
    assert_eq!(child_head, Some(committed_txn_id));
    assert_ne!(child_head, Some(open_txn_id));
}
