//! P5 — Two branches that both write the same `logical_path` after
//! the LCA produce a conflict entry in the compare response.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn open_commit_with_file(
    h: &common::Harness,
    dataset_id: Uuid,
    branch: &str,
    logical_path: &str,
) -> Uuid {
    let req = Request::builder()
        .method("POST")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/{branch}/transactions"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({"type": "SNAPSHOT", "providence": {}})).unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "open: {}", resp.status());
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let txn_id = Uuid::parse_str(body["id"].as_str().unwrap()).unwrap();

    sqlx::query(
        r#"INSERT INTO dataset_transaction_files
              (transaction_id, logical_path, physical_path, size_bytes, op)
           VALUES ($1, $2, $3, 8, 'ADD')"#,
    )
    .bind(txn_id)
    .bind(logical_path)
    .bind(format!("s3://test/{txn_id}/{logical_path}"))
    .execute(&h.pool)
    .await
    .expect("stage file");

    let commit_req = Request::builder()
        .method("POST")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/{branch}/transactions/{txn_id}:commit"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(b"{}".to_vec()))
        .unwrap();
    let resp = h.router.clone().oneshot(commit_req).await.expect("router");
    assert!(resp.status().is_success(), "commit: {}", resp.status());
    txn_id
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn overlapping_writes_on_two_branches_show_up_as_conflicts() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.compare-conflicts")
            .await;

    // Create two siblings off master, then commit overlapping
    // writes on each.
    for name in ["feature-a", "feature-b"] {
        let req = Request::builder()
            .method("POST")
            .uri(format!("/v1/datasets/{dataset_id}/branches"))
            .header("authorization", format!("Bearer {}", h.token))
            .header("content-type", "application/json")
            .body(Body::from(
                serde_json::to_vec(&json!({ "name": name, "parent_branch": "master" })).unwrap(),
            ))
            .unwrap();
        let resp = h.router.clone().oneshot(req).await.expect("router");
        assert!(resp.status().is_success());
    }

    let _a_tx = open_commit_with_file(&h, dataset_id, "feature-a", "data/users.parquet").await;
    let _b_tx = open_commit_with_file(&h, dataset_id, "feature-b", "data/users.parquet").await;
    // Disjoint write — shouldn't surface as a conflict.
    let _b_tx_disjoint =
        open_commit_with_file(&h, dataset_id, "feature-b", "data/products.parquet").await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/compare?base=feature-a&compare=feature-b"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    let conflicts: Vec<Value> = body["conflicting_files"].as_array().cloned().unwrap();
    assert_eq!(conflicts.len(), 1);
    assert_eq!(conflicts[0]["logical_path"], "data/users.parquet");
    assert!(
        conflicts[0]["a_transaction_rid"]
            .as_str()
            .map(|s| s.starts_with("ri.foundry.main.transaction."))
            .unwrap_or(false)
    );

    // Per-side counters: A has one diverged commit, B has two.
    assert_eq!(body["a_only_transactions"].as_array().unwrap().len(), 1);
    assert_eq!(body["b_only_transactions"].as_array().unwrap().len(), 2);
}
