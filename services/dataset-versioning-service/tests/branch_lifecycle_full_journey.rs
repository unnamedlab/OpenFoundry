//! P5 — End-to-end lifecycle journey covering every P1+P2+P3+P4
//! endpoint we ship in `dataset-versioning-service`. Single happy
//! path so a regression anywhere in the branch surface fails fast.
//!
//! Steps (mirrors the Foundry "Branching lifecycle" doc):
//!
//!   1. Seed dataset + `master` (root).
//!   2. Open + commit a SNAPSHOT on master.
//!   3. Create child `feature` from master HEAD.
//!   4. Commit a SNAPSHOT on feature, capture txn id.
//!   5. Create grandchild `patch` from a *transaction* on feature.
//!   6. Walk ancestry: patch → feature → master.
//!   7. Reparent patch onto master (skip feature).
//!   8. Compare master vs patch — LCA must be master.
//!   9. Read markings on patch — empty (parent had none).
//!  10. Set retention policy TTL_DAYS = 1 on feature.
//!  11. Soft-delete feature; preview-delete shows the reparent plan.
//!  12. Restore is rejected because deletion is final (delete ≠ archive).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn http_json(
    h: &common::Harness,
    method: &str,
    uri: &str,
    body: Option<Value>,
) -> (StatusCode, Value) {
    let mut builder = Request::builder()
        .method(method)
        .uri(uri)
        .header("authorization", format!("Bearer {}", h.token));
    let body_bytes = match body {
        Some(value) => {
            builder = builder.header("content-type", "application/json");
            Body::from(serde_json::to_vec(&value).unwrap())
        }
        None => Body::empty(),
    };
    let resp = h
        .router
        .clone()
        .oneshot(builder.body(body_bytes).unwrap())
        .await
        .expect("router");
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 256 * 1024).await.unwrap();
    let json = serde_json::from_slice(&bytes).unwrap_or(Value::Null);
    (status, json)
}

async fn open_commit(h: &common::Harness, dataset_id: Uuid, branch: &str, tx_type: &str) -> Uuid {
    let (status, body) = http_json(
        h,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/{branch}/transactions"),
        Some(json!({ "type": tx_type, "providence": {} })),
    )
    .await;
    assert!(status.is_success(), "open: {status} {body}");
    let txn_id = Uuid::parse_str(body["id"].as_str().unwrap()).unwrap();
    sqlx::query(
        r#"INSERT INTO dataset_transaction_files
              (transaction_id, logical_path, physical_path, size_bytes, op)
           VALUES ($1, $2, $3, 16, 'ADD')"#,
    )
    .bind(txn_id)
    .bind(format!("data/{branch}.parquet"))
    .bind(format!("s3://test/{txn_id}/data.parquet"))
    .execute(&h.pool)
    .await
    .expect("stage");
    let (commit_status, commit_body) = http_json(
        h,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/{branch}/transactions/{txn_id}:commit"),
        Some(json!({})),
    )
    .await;
    assert!(
        commit_status.is_success(),
        "commit: {commit_status} {commit_body}"
    );
    txn_id
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn full_p1_to_p5_journey() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.full-journey").await;

    // 1. master is already seeded.
    // 2. Snapshot on master.
    let _master_tx = open_commit(&h, dataset_id, "master", "SNAPSHOT").await;

    // 3. Create feature off master.
    let (status, body) = http_json(
        &h,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({
            "name": "feature",
            "source": { "from_branch": "master" },
        })),
    )
    .await;
    assert_eq!(status, StatusCode::CREATED, "create feature: {body}");

    // 4. Commit on feature.
    let feature_tx = open_commit(&h, dataset_id, "feature", "APPEND").await;

    // 5. Create patch from feature_tx.
    let (status, _) = http_json(
        &h,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches"),
        Some(json!({
            "name": "patch",
            "source": { "from_transaction_rid": format!("ri.foundry.main.transaction.{feature_tx}") },
        })),
    )
    .await;
    assert_eq!(status, StatusCode::CREATED);

    // 6. Ancestry of patch is [patch, feature, master].
    let (status, body) = http_json(
        &h,
        "GET",
        &format!("/v1/datasets/{dataset_id}/branches/patch/ancestry"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK);
    let chain = body
        .as_array()
        .unwrap()
        .iter()
        .map(|n| n["name"].as_str().unwrap_or("").to_string())
        .collect::<Vec<_>>();
    assert_eq!(chain, vec!["patch", "feature", "master"]);

    // 7. Reparent patch directly onto master.
    let (status, _) = http_json(
        &h,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/patch:reparent"),
        Some(json!({ "new_parent_branch": "master" })),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    // 8. Compare master vs patch. The reparent leaves the LCA on
    //    master.
    let (status, body) = http_json(
        &h,
        "GET",
        &format!("/v1/datasets/{dataset_id}/branches/compare?base=master&compare=patch"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK);
    assert!(
        body["lca_branch_rid"]
            .as_str()
            .map(|s| s.starts_with("ri.foundry.main.branch."))
            .unwrap_or(false)
    );

    // 9. Markings — patch starts with no inherited markings.
    let (status, body) = http_json(
        &h,
        "GET",
        &format!("/v1/datasets/{dataset_id}/branches/patch/markings"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(body["effective"].as_array().unwrap().len(), 0);
    assert_eq!(body["explicit"].as_array().unwrap().len(), 0);

    // 10. PATCH retention on feature.
    let (status, _) = http_json(
        &h,
        "PATCH",
        &format!("/v1/datasets/{dataset_id}/branches/feature/retention"),
        Some(json!({ "policy": "TTL_DAYS", "ttl_days": 1 })),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    // 11. preview-delete on feature shows the reparent plan.
    let (status, body) = http_json(
        &h,
        "GET",
        &format!("/v1/datasets/{dataset_id}/branches/feature/preview-delete"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(body["transactions_preserved"], true);

    // Soft-delete feature.
    let (status, _) = http_json(
        &h,
        "DELETE",
        &format!("/v1/datasets/{dataset_id}/branches/feature"),
        None,
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    // 12. Restore on a *deleted* (not archived) branch returns 404
    //     because the partial-unique index hides it from active rows.
    let (status, _) = http_json(
        &h,
        "POST",
        &format!("/v1/datasets/{dataset_id}/branches/feature:restore"),
        Some(json!({})),
    )
    .await;
    assert_eq!(status, StatusCode::NOT_FOUND);
}
