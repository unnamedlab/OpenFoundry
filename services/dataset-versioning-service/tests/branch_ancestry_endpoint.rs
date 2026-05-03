//! P1 — `GET /v1/datasets/{rid}/branches/{branch}/ancestry` walks
//! up the parent chain from the requested branch to the root.

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

async fn create_child(
    router: &axum::Router,
    token: &str,
    dataset_id: Uuid,
    name: &str,
    parent: &str,
) {
    let (status, body) = req(
        router,
        token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({ "name": name, "source": { "from_branch": parent } })),
    )
    .await;
    assert!(status.is_success(), "create {name}: {status} {body}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn ancestry_endpoint_walks_up_to_root() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.ancestry",
    )
    .await;

    create_child(&h.router, &h.token, dataset_id, "develop", "master").await;
    create_child(&h.router, &h.token, dataset_id, "feature", "develop").await;
    create_child(&h.router, &h.token, dataset_id, "patch", "feature").await;

    let (status, body) = req(
        &h.router,
        &h.token,
        "GET",
        &format!("/v1/datasets/{dataset_id}/branches/patch/ancestry"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK, "ancestry: {status} {body}");
    let chain = body.as_array().cloned().expect("array");
    let names: Vec<String> = chain
        .iter()
        .map(|n| n["name"].as_str().unwrap_or("").to_string())
        .collect();
    assert_eq!(
        names,
        vec!["patch", "feature", "develop", "master"],
        "ancestry must walk child -> root"
    );
    let last = chain.last().expect("at least one ancestor");
    assert_eq!(last["is_root"], json!(true), "last must be root");
    assert!(
        last["rid"].as_str().unwrap_or("").starts_with("ri.foundry.main.branch."),
        "rid must follow the foundry shape, got {:?}",
        last["rid"]
    );
}
