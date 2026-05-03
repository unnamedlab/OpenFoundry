//! P6 — Application reference: cursor pagination on list endpoints.
//!
//! Verifies that `GET /v1/datasets/{rid}/transactions` returns the
//! Foundry-style envelope `{ data, next_cursor, has_more }` and that
//! `?cursor=&limit=` walks the collection without losing or repeating
//! rows. Docker-gated.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use serde_json::Value;
use tower::ServiceExt;

async fn seed_n_committed_transactions(
    pool: &sqlx::PgPool,
    dataset_id: uuid::Uuid,
    branch_id: uuid::Uuid,
    n: usize,
) {
    for i in 0..n {
        let tx_id = uuid::Uuid::now_v7();
        sqlx::query(
            r#"INSERT INTO dataset_transactions
                  (id, dataset_id, branch_id, branch_name, tx_type, status,
                   summary, started_at, committed_at)
               VALUES ($1, $2, $3, 'master', 'APPEND', 'COMMITTED',
                       $4, NOW() - ($5 || ' minutes')::interval,
                       NOW() - ($5 || ' minutes')::interval)"#,
        )
        .bind(tx_id)
        .bind(dataset_id)
        .bind(branch_id)
        .bind(format!("seed-{i}"))
        .bind(i as i64)
        .execute(pool)
        .await
        .expect("seed transaction");
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn list_transactions_returns_page_envelope_and_walks_cursor() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.pagination";
    let dataset_id = common::seed_dataset_with_master(&h.pool, rid).await;
    let branch_id: uuid::Uuid = sqlx::query_scalar(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();

    seed_n_committed_transactions(&h.pool, dataset_id, branch_id, 7).await;

    // Page 1: limit=3, no cursor → first 3 rows + has_more=true.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/transactions?limit=3"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    assert!(body["data"].is_array(), "response must carry `data` array: {body}");
    let page1 = body["data"].as_array().unwrap();
    assert_eq!(page1.len(), 3, "limit=3 ⇒ 3 rows");
    assert_eq!(body["has_more"], true);
    assert!(body["next_cursor"].is_string(), "has_more=true ⇒ cursor present");
    let cursor = body["next_cursor"].as_str().unwrap().to_string();

    // Page 2: feed back next_cursor → next 3 rows, still has_more.
    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{rid}/transactions?limit=3&cursor={cursor}"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body2: Value = serde_json::from_slice(&bytes).unwrap();
    let page2 = body2["data"].as_array().unwrap();
    assert_eq!(page2.len(), 3);
    assert_eq!(body2["has_more"], true);

    // Page 3: limit=3 from offset 6 → only 1 row left, has_more=false.
    let cursor2 = body2["next_cursor"].as_str().unwrap().to_string();
    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{rid}/transactions?limit=3&cursor={cursor2}"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body3: Value = serde_json::from_slice(&bytes).unwrap();
    let page3 = body3["data"].as_array().unwrap();
    assert_eq!(page3.len(), 1);
    assert_eq!(body3["has_more"], false);
    assert!(body3.get("next_cursor").is_none() || body3["next_cursor"].is_null());

    // Pages must not overlap.
    let id1: Vec<&str> = page1.iter().map(|v| v["id"].as_str().unwrap()).collect();
    let id2: Vec<&str> = page2.iter().map(|v| v["id"].as_str().unwrap()).collect();
    let id3: Vec<&str> = page3.iter().map(|v| v["id"].as_str().unwrap()).collect();
    for x in &id2 {
        assert!(!id1.contains(x), "page 2 must not repeat ids from page 1");
    }
    for x in &id3 {
        assert!(!id2.contains(x) && !id1.contains(x), "page 3 must not repeat");
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn list_branches_returns_page_envelope() {
    let h = common::spawn().await;
    let rid = "ri.foundry.main.dataset.branches.page";
    common::seed_dataset_with_master(&h.pool, rid).await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{rid}/branches"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert!(body["data"].is_array(), "branches list must use Page envelope: {body}");
    assert_eq!(body["has_more"], false, "single branch ⇒ has_more=false");
}
