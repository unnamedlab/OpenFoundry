//! P3 — `GET /v1/datasets/{rid}/files/{file_id}/download` returns a
//! 302 to a presigned URL and emits a `files.download` audit event.
//! Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use serde_json::Value;
use sqlx::PgPool;
use tower::ServiceExt;
use uuid::Uuid;

async fn seed_one_file_via_committed_txn(pool: &PgPool, dataset_id: Uuid) -> Uuid {
    let branch_id = sqlx::query_scalar::<_, Uuid>(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(pool)
    .await
    .unwrap();

    let txn_id = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at)
           VALUES ($1, $2, $3, 'master', 'SNAPSHOT', 'OPEN', '', NOW())"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .unwrap();
    sqlx::query(
        r#"INSERT INTO dataset_transaction_files
              (transaction_id, logical_path, physical_path, size_bytes, op)
           VALUES ($1, $2, $3, 100, 'ADD')"#,
    )
    .bind(txn_id)
    .bind("downloads/file.parquet")
    .bind("foundry/datasets/downloads/file.parquet")
    .execute(pool)
    .await
    .unwrap();
    sqlx::query(
        "UPDATE dataset_transactions SET status = 'COMMITTED', committed_at = NOW() WHERE id = $1",
    )
    .bind(txn_id)
    .execute(pool)
    .await
    .unwrap();
    sqlx::query(
        "UPDATE dataset_branches SET head_transaction_id = $1, updated_at = NOW() WHERE id = $2",
    )
    .bind(txn_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .unwrap();

    sqlx::query_scalar::<_, Uuid>(
        "SELECT id FROM dataset_files WHERE transaction_id = $1 AND logical_path = $2",
    )
    .bind(txn_id)
    .bind("downloads/file.parquet")
    .fetch_one(pool)
    .await
    .unwrap()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn download_returns_302_to_signed_url_with_expires_and_sig() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.download").await;
    let file_id = seed_one_file_via_committed_txn(&h.pool, dataset_id).await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/files/{file_id}/download"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status().as_u16(), 302, "302 redirect to presigned URL");
    let location = resp
        .headers()
        .get(axum::http::header::LOCATION)
        .expect("Location header")
        .to_str()
        .unwrap();

    // Local presign URL carries `expires=` + `sig=` query params.
    let parsed = url::Url::parse(location).expect("valid Location URL");
    let q: std::collections::HashMap<_, _> = parsed.query_pairs().into_owned().collect();
    let expires_at: i64 = q
        .get("expires")
        .expect("expires query param")
        .parse()
        .unwrap();
    assert!(expires_at > chrono::Utc::now().timestamp(), "ttl must be future");
    let sig = q.get("sig").expect("sig query param");
    assert!(!sig.is_empty(), "signature must be non-empty");

    // The signature is verifiable against the AppState's backing FS.
    let key = parsed.path().trim_start_matches("/v1/_internal/local-fs/");
    assert!(
        h.backing_fs.verify_local_signature(key, expires_at, sig),
        "presigned URL signature must verify against the backing fs"
    );

    // Drain the body just to be tidy.
    let _ = to_bytes(resp.into_body(), 1024).await.ok();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn upload_url_endpoint_returns_signed_put_for_open_transaction() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.upload-url").await;
    let branch_id = sqlx::query_scalar::<_, Uuid>(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();

    // Open a transaction (status=OPEN). The upload-url endpoint
    // requires OPEN; commit / abort would be rejected.
    let txn_id = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at)
           VALUES ($1, $2, $3, 'master', 'APPEND', 'OPEN', '', NOW())"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .bind(branch_id)
    .execute(&h.pool)
    .await
    .unwrap();

    let req = Request::builder()
        .method("POST")
        .uri(format!(
            "/v1/datasets/{dataset_id}/transactions/{txn_id}/files"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&serde_json::json!({
                "logical_path": "user-supplied/data.parquet",
                "content_type": "application/x-parquet",
                "sha256": "abc"
            }))
            .unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success(), "upload-url ok: {}", resp.status());
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["method"], "PUT");
    let url = body["url"].as_str().expect("url field").to_string();
    assert!(url.contains("expires=") && url.contains("sig="));
    let physical_uri = body["physical_uri"].as_str().unwrap();
    assert!(
        physical_uri.contains(&txn_id.to_string()),
        "physical URI must scope by transaction id: {physical_uri}"
    );
}
