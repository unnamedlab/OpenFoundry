//! Foundry dataset versioning semantics: edge cases around transactions,
//! fallbacks, rollback permissions and concurrent commits.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn request_json(
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
    let body = match body {
        Some(value) => {
            builder = builder.header("content-type", "application/json");
            Body::from(serde_json::to_vec(&value).unwrap())
        }
        None => Body::empty(),
    };
    let resp = router
        .clone()
        .oneshot(builder.body(body).unwrap())
        .await
        .unwrap();
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let json = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    (status, json)
}

async fn create_branch(h: &common::Harness, dataset_id: Uuid, name: &str) {
    let (status, body) = request_json(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({ "name": name, "parent_branch": "master" })),
    )
    .await;
    assert!(status.is_success(), "create branch {name}: {status} {body}");
}

async fn open_txn(h: &common::Harness, dataset_id: Uuid, branch: &str, tx_type: &str) -> Uuid {
    let (status, body) = request_json(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/{branch}/transactions"),
        Some(json!({ "type": tx_type, "providence": {}, "summary": tx_type })),
    )
    .await;
    assert!(status.is_success(), "open txn: {status} {body}");
    Uuid::parse_str(body["id"].as_str().expect("transaction id")).unwrap()
}

async fn stage_file(pool: &sqlx::PgPool, txn_id: Uuid, logical_path: &str, op: &str) {
    sqlx::query(
        r#"INSERT INTO dataset_transaction_files
             (transaction_id, logical_path, physical_path, size_bytes, op)
           VALUES ($1, $2, $3, 10, $4)"#,
    )
    .bind(txn_id)
    .bind(logical_path)
    .bind(format!("s3://bucket/{txn_id}/{logical_path}"))
    .bind(op)
    .execute(pool)
    .await
    .expect("stage file");
}

async fn commit(h: &common::Harness, dataset_id: Uuid, branch: &str, txn_id: Uuid) -> StatusCode {
    request_json(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/{branch}/transactions/{txn_id}:commit"),
        Some(json!({})),
    )
    .await
    .0
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn double_commit_returns_conflict() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.double-commit").await;
    let txn_id = open_txn(&h, dataset_id, "master", "SNAPSHOT").await;
    stage_file(&h.pool, txn_id, "data/part-0.parquet", "ADD").await;

    assert_eq!(
        commit(&h, dataset_id, "master", txn_id).await,
        StatusCode::OK
    );
    let (status, body) = request_json(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/transactions/{txn_id}:commit"),
        Some(json!({})),
    )
    .await;

    assert_eq!(status, StatusCode::CONFLICT, "second commit: {body}");
    assert_eq!(body["current_state"], "COMMITTED");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn concurrent_commit_on_same_transaction_has_single_winner() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.concurrent-commit")
            .await;
    let txn_id = open_txn(&h, dataset_id, "master", "SNAPSHOT").await;
    stage_file(&h.pool, txn_id, "data/part-0.parquet", "ADD").await;

    let uri = format!("/v1/datasets/{dataset_id}/branches/master/transactions/{txn_id}:commit");
    let a = request_json(&h.router, &h.token, "POST", &uri, Some(json!({})));
    let b = request_json(&h.router, &h.token, "POST", &uri, Some(json!({})));
    let ((a_status, _), (b_status, _)) = tokio::join!(a, b);

    assert!(
        (a_status == StatusCode::OK && b_status == StatusCode::CONFLICT)
            || (a_status == StatusCode::CONFLICT && b_status == StatusCode::OK),
        "expected one winner and one conflict, got {a_status} and {b_status}"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn abort_is_idempotent() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.abort-idempotent").await;
    let txn_id = open_txn(&h, dataset_id, "master", "APPEND").await;
    let uri = format!("/v1/datasets/{dataset_id}/branches/master/transactions/{txn_id}:abort");

    let (first_status, _) = request_json(&h.router, &h.token, "POST", &uri, Some(json!({}))).await;
    let (second_status, body) =
        request_json(&h.router, &h.token, "POST", &uri, Some(json!({}))).await;

    assert_eq!(first_status, StatusCode::OK);
    assert_eq!(second_status, StatusCode::OK);
    assert_eq!(body["status"], "ABORTED");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn missing_branch_returns_404() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.missing-branch").await;

    let (status, body) = request_json(
        &h.router,
        &h.token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/does-not-exist/transactions"),
        Some(json!({ "type": "SNAPSHOT", "providence": {} })),
    )
    .await;

    assert_eq!(status, StatusCode::NOT_FOUND, "missing branch: {body}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn circular_fallback_is_rejected() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.fallback-cycle").await;
    create_branch(&h, dataset_id, "feature").await;
    create_branch(&h, dataset_id, "develop").await;

    let (status, body) = request_json(
        &h.router,
        &h.token,
        "PUT",
        &format!("/v1/datasets/{dataset_id}/branches/develop/fallbacks"),
        Some(json!({ "chain": ["feature"] })),
    )
    .await;
    assert!(status.is_success(), "set develop fallback: {status} {body}");

    let (cycle_status, cycle_body) = request_json(
        &h.router,
        &h.token,
        "PUT",
        &format!("/v1/datasets/{dataset_id}/branches/feature/fallbacks"),
        Some(json!({ "chain": ["develop"] })),
    )
    .await;
    assert_eq!(cycle_status, StatusCode::BAD_REQUEST);
    assert_eq!(cycle_body["error"], "fallback chain contains a cycle");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn rollback_requires_dataset_write_permission() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.rollback-permission")
            .await;
    let txn_id = open_txn(&h, dataset_id, "master", "SNAPSHOT").await;
    stage_file(&h.pool, txn_id, "data/part-0.parquet", "ADD").await;
    assert_eq!(
        commit(&h, dataset_id, "master", txn_id).await,
        StatusCode::OK
    );

    let weak_claims = auth_middleware::jwt::build_access_claims(
        &h.jwt_config,
        Uuid::now_v7(),
        "reader@openfoundry.test",
        "Dataset Reader",
        vec![],
        vec!["dataset.read".into()],
        None,
        Value::Null,
        vec!["password".into()],
    );
    let weak_token =
        auth_middleware::jwt::encode_token(&h.jwt_config, &weak_claims).expect("weak token");
    let (status, body) = request_json(
        &h.router,
        &weak_token,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/master/rollback"),
        Some(json!({ "transaction_id": txn_id })),
    )
    .await;

    assert_eq!(status, StatusCode::FORBIDDEN, "rollback permission: {body}");
}
