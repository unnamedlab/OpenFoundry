//! P3 — `GET /v1/datasets/{rid}/files` honours the Foundry view
//! algorithm: SNAPSHOT replaces; APPEND adds; UPDATE adds/replaces;
//! DELETE removes (soft, surfaced as `status='deleted'`).
//!
//! The test seeds a 4-transaction history and asserts the resulting
//! file set + per-file status. Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use serde_json::Value;
use sqlx::PgPool;
use tower::ServiceExt;
use uuid::Uuid;

/// Insert a committed transaction with a list of staged files. `files`
/// is a list of `(logical_path, op)` tuples; `op` ∈ ADD|REPLACE|REMOVE.
/// The trigger from migration 20260503000002 fires on COMMITTED status
/// transitions, so we INSERT as OPEN then UPDATE to COMMITTED.
async fn commit_transaction_with_files(
    pool: &PgPool,
    dataset_id: Uuid,
    branch_id: Uuid,
    tx_type: &str,
    files: &[(&str, &str)],
) -> Uuid {
    let txn_id = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at)
           VALUES ($1, $2, $3, 'master', $4, 'OPEN', '', NOW())"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .bind(branch_id)
    .bind(tx_type)
    .execute(pool)
    .await
    .expect("seed open txn");

    for (i, (logical, op)) in files.iter().enumerate() {
        let physical_path = format!("foundry/datasets/{txn_id}/{i:02}-{logical}");
        sqlx::query(
            r#"INSERT INTO dataset_transaction_files
                  (transaction_id, logical_path, physical_path, size_bytes, op)
               VALUES ($1, $2, $3, $4, $5)"#,
        )
        .bind(txn_id)
        .bind(logical)
        .bind(&physical_path)
        .bind((logical.len() * 10) as i64)
        .bind(op)
        .execute(pool)
        .await
        .expect("stage file");
    }

    // Flip OPEN → COMMITTED. The trigger projects rows into dataset_files.
    sqlx::query(
        r#"UPDATE dataset_transactions
              SET status = 'COMMITTED', committed_at = NOW()
            WHERE id = $1"#,
    )
    .bind(txn_id)
    .execute(pool)
    .await
    .expect("commit");

    // Advance branch HEAD so /files resolves to this txn.
    sqlx::query(
        "UPDATE dataset_branches SET head_transaction_id = $1, updated_at = NOW() WHERE id = $2",
    )
    .bind(txn_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .expect("advance HEAD");

    txn_id
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn snapshot_append_update_delete_resolve_to_view_effective_files() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.files-algo").await;
    let branch_id = sqlx::query_scalar::<_, Uuid>(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();

    // Doc example § "Example of transaction types":
    //   1) SNAPSHOT contains files A and B
    //   2) APPEND adds C
    //   3) UPDATE replaces A with A'
    //   4) DELETE removes B
    //  ⇒ effective view = { A', C }, with B and A surfaced as deleted.
    commit_transaction_with_files(
        &h.pool,
        dataset_id,
        branch_id,
        "SNAPSHOT",
        &[("file_a", "ADD"), ("file_b", "ADD")],
    )
    .await;
    commit_transaction_with_files(
        &h.pool,
        dataset_id,
        branch_id,
        "APPEND",
        &[("file_c", "ADD")],
    )
    .await;
    commit_transaction_with_files(
        &h.pool,
        dataset_id,
        branch_id,
        "UPDATE",
        &[("file_a", "REPLACE")],
    )
    .await;
    commit_transaction_with_files(
        &h.pool,
        dataset_id,
        branch_id,
        "DELETE",
        &[("file_b", "REMOVE")],
    )
    .await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{dataset_id}/files"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    let files = body["files"].as_array().expect("files array");
    let mut active: Vec<&str> = files
        .iter()
        .filter(|f| f["status"] == "active")
        .map(|f| f["logical_path"].as_str().unwrap())
        .collect();
    active.sort();
    assert_eq!(
        active,
        vec!["file_a", "file_c"],
        "effective view must be {{file_a (replaced), file_c}}: {body}"
    );

    let mut deleted: Vec<&str> = files
        .iter()
        .filter(|f| f["status"] == "deleted")
        .map(|f| f["logical_path"].as_str().unwrap())
        .collect();
    deleted.sort();
    assert!(
        deleted.contains(&"file_b"),
        "file_b removed by DELETE must appear with status=deleted: {body}"
    );

    // Prefix filter narrows the result without breaking the view algo.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{dataset_id}/files?prefix=file_a"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let names: Vec<&str> = body["files"]
        .as_array()
        .unwrap()
        .iter()
        .map(|f| f["logical_path"].as_str().unwrap())
        .collect();
    assert!(names.iter().all(|n| n.starts_with("file_a")));
}
