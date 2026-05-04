//! P4 — `POST /global-branches/{id}/promote` transactionally enqueues
//! a `global.branch.promote.requested.v1` outbox event.
//!
//! The outbox helper does INSERT+DELETE in the same transaction, so
//! steady-state row count is 0. The test asserts the HTTP path
//! returns 200 + the canonical event_id / topic, which means the
//! outbox call succeeded (any failure would have rolled back).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use global_branch_service::global::{model::CreateGlobalBranchRequest, store};
use serde_json::Value;
use tower::ServiceExt;
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn promote_writes_outbox_event_and_returns_event_id() {
    let h = common::spawn().await;

    let row = store::create_branch(
        &h.pool,
        &CreateGlobalBranchRequest {
            name: "release-2026-Q3".into(),
            description: None,
            parent_global_branch: None,
        },
        "tester",
    )
    .await
    .unwrap();

    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/global-branches/{}/promote", row.id))
        .header("content-type", "application/json")
        .body(Body::from("{}"))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["topic"], "foundry.global.branch.promote.requested.v1");
    let event_id = body["event_id"].as_str().expect("event_id");
    assert!(Uuid::parse_str(event_id).is_ok());

    // Outbox steady-state — the in-tx DELETE keeps the table empty.
    let count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM outbox.events")
        .fetch_one(&h.pool)
        .await
        .unwrap();
    assert_eq!(count, 0);
}
