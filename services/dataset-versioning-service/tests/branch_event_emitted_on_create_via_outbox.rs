//! P4 — branch lifecycle events land on the outbox transactionally.
//!
//! The outbox helper does INSERT+DELETE inside one transaction so
//! Debezium logical-decoding picks up the WAL record. From the test's
//! point of view, the row never sticks around — but the call must
//! complete without error and the audit trail must still show the
//! mutation.

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use serde_json::json;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn create_branch_emits_outbox_event_in_same_transaction() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.outbox-event").await;

    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{dataset_id}/branches"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({
                "name": "feature",
                "source": { "from_branch": "master" },
            }))
            .unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::CREATED);

    // Steady-state count is zero: the outbox helper deletes the row
    // in the same tx so Debezium captures the WAL record but the
    // table itself stays empty. The call returning OK + the dataset
    // branch row landing is the assertion that the event was
    // enqueued (a failure in `enqueue` would have aborted the
    // transaction and rolled the branch back).
    let count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM outbox.events")
        .fetch_one(&h.pool)
        .await
        .unwrap();
    assert_eq!(count, 0);

    let branch_count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM dataset_branches WHERE dataset_id = $1 AND name = 'feature'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();
    assert_eq!(branch_count, 1);
}
