//! T6.x — schema-per-view: two consecutive commits with different
//! schemas must each land in their own `dataset_view_schemas` row,
//! mirroring Foundry's "schemas are metadata on a *dataset view*"
//! contract (`Datasets.md` § "Schemas").
//!
//! The test drives the service end-to-end via the HTTP router (so the
//! trigger fires) and checks the row count + `content_hash` divergence
//! straight from the seeded Postgres. Gated behind `#[ignore]` so
//! `cargo test` stays Docker-free.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn two_commits_with_different_schemas_land_in_separate_view_rows() {
    let h = common::spawn().await;
    let id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.schema-per-view").await;

    let schema_v1 = json!({
        "fields": [
            { "name": "id",   "type": "LONG",   "nullable": false },
            { "name": "name", "type": "STRING", "nullable": true  }
        ],
        "file_format": "PARQUET",
        "custom_metadata": null
    });
    let schema_v2 = json!({
        "fields": [
            { "name": "id",     "type": "LONG",   "nullable": false },
            { "name": "name",   "type": "STRING", "nullable": true  },
            // New column → schema evolution; this commit must produce a
            // distinct dataset_view_schemas row.
            { "name": "amount", "type": "DECIMAL", "precision": 18, "scale": 4, "nullable": true }
        ],
        "file_format": "PARQUET",
        "custom_metadata": null
    });

    let txn_first = open_commit_via_pool(&h, id, "SNAPSHOT", &schema_v1).await;
    let txn_second = open_commit_via_pool(&h, id, "APPEND", &schema_v2).await;

    // Two distinct rows → two views, two schemas with different hashes.
    let rows: Vec<(uuid::Uuid, String)> = sqlx::query_as(
        r#"SELECT vs.view_id, vs.content_hash
             FROM dataset_view_schemas vs
             JOIN dataset_views v ON v.id = vs.view_id
            WHERE v.dataset_id = $1
            ORDER BY vs.created_at ASC"#,
    )
    .bind(id)
    .fetch_all(&h.pool)
    .await
    .expect("query schemas");

    assert!(
        rows.len() >= 2,
        "expected at least 2 schemas, got {}: {:?}",
        rows.len(),
        rows
    );
    assert_ne!(
        rows[0].1, rows[1].1,
        "two distinct schemas must hash differently (txns: {txn_first} -> {txn_second})"
    );

    // Sanity: each schema row's view points at the matching head txn.
    let view_to_txn: Vec<(uuid::Uuid, uuid::Uuid)> = sqlx::query_as(
        r#"SELECT v.id, v.head_transaction_id
             FROM dataset_views v
             JOIN dataset_view_schemas vs ON vs.view_id = v.id
            WHERE v.dataset_id = $1
            ORDER BY v.computed_at ASC"#,
    )
    .bind(id)
    .fetch_all(&h.pool)
    .await
    .unwrap();
    let head_txns: std::collections::HashSet<_> = view_to_txn.iter().map(|(_, t)| *t).collect();
    assert!(
        head_txns.contains(&txn_first) && head_txns.contains(&txn_second),
        "view rows must cover both committed transactions: {view_to_txn:?}"
    );
}

/// In-test variant of `open_commit_with_schema` that uses the harness
/// pool directly to inject `metadata.schema` (avoiding the global cell).
async fn open_commit_via_pool(
    h: &common::Harness,
    dataset_id: uuid::Uuid,
    tx_type: &str,
    schema: &Value,
) -> uuid::Uuid {
    let req = Request::builder()
        .method("POST")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/master/transactions"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({ "type": tx_type, "providence": {} })).unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "open {tx_type}");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let txn_id: uuid::Uuid = body["id"].as_str().unwrap().parse().unwrap();

    sqlx::query(
        r#"UPDATE dataset_transactions
              SET metadata = metadata || jsonb_build_object('schema', $2::jsonb)
            WHERE id = $1"#,
    )
    .bind(txn_id)
    .bind(schema)
    .execute(&h.pool)
    .await
    .expect("inject schema");

    let req = Request::builder()
        .method("POST")
        .uri(format!(
            "/v1/datasets/{dataset_id}/branches/master/transactions/{txn_id}:commit"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(b"{}".to_vec()))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "commit {tx_type}: {resp:?}");
    txn_id
}
