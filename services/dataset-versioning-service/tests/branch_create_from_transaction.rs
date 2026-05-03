//! P1 — child branch from a specific COMMITTED transaction.
//!
//! Given a master branch with two committed SNAPSHOTs T1 and T2, a
//! child branch created via `source.from_transaction_rid = T2.rid`
//! must carry `head_transaction_id = T2.id` and a default
//! `fallback_chain = ["master"]`.

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

async fn open_commit(
    router: &axum::Router,
    token: &str,
    dataset_id: Uuid,
    branch: &str,
    tx_type: &str,
) -> Uuid {
    let (status, body) = req(
        router,
        token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/{branch}/transactions"),
        Some(json!({ "type": tx_type, "providence": {} })),
    )
    .await;
    assert!(status.is_success(), "open: {status} {body}");
    let txn_id = Uuid::parse_str(body["id"].as_str().unwrap()).unwrap();
    let (commit_status, commit_body) = req(
        router,
        token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/{branch}/transactions/{txn_id}:commit"),
        Some(json!({})),
    )
    .await;
    assert!(commit_status.is_success(), "commit: {commit_status} {commit_body}");
    txn_id
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn child_branch_from_committed_transaction_points_at_it() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.branch-from-tx",
    )
    .await;

    let _t1 = open_commit(&h.router, &h.token, dataset_id, "master", "SNAPSHOT").await;
    let t2 = open_commit(&h.router, &h.token, dataset_id, "master", "APPEND").await;
    let t2_rid = format!("ri.foundry.main.transaction.{t2}");

    let (status, body) = req(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({
            "name": "feature",
            "source": { "from_transaction_rid": t2_rid }
        })),
    )
    .await;
    assert_eq!(status, StatusCode::CREATED, "create: {status} {body}");
    assert_eq!(
        body["head_transaction_id"].as_str().and_then(|s| Uuid::parse_str(s).ok()),
        Some(t2),
        "head must equal the source transaction"
    );
    assert_eq!(
        body["created_from_transaction_id"]
            .as_str()
            .and_then(|s| Uuid::parse_str(s).ok()),
        Some(t2),
        "created_from must equal the source transaction"
    );
    assert_eq!(
        body["fallback_chain"],
        json!(["master"]),
        "default fallback chain inherits the source branch name"
    );
}
