//! Foundry transactional-media-set semantics ("Advanced media set settings.md"):
//!
//! * Items written inside an OPEN transaction are tied to that transaction.
//! * Committing the transaction seals it; further commits are rejected.
//! * Aborting discards the staged items so a re-open starts clean.
//! * A media set with `TRANSACTIONLESS` policy MUST refuse to open
//!   transactions.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use media_sets_service::models::TransactionPolicy;
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test]
async fn transactional_open_commit_seals_writes_and_rejects_double_commit() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "transactional-screens",
        "ri.foundry.main.project.proj-tx",
        TransactionPolicy::Transactional,
    )
    .await;

    // ── 1. Open a transaction ──────────────────────────────────────
    let txn_resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/media-sets/{}/transactions", set.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(json!({}).to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(txn_resp.status(), StatusCode::CREATED);
    let txn: Value = serde_json::from_slice(&txn_resp.into_body().collect().await.unwrap().to_bytes()).unwrap();
    let txn_rid = txn["rid"].as_str().unwrap().to_string();
    assert_eq!(txn["state"], "OPEN");

    // ── 2. Stage an item inside the transaction ────────────────────
    let upload = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/media-sets/{}/items/upload-url", set.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(
                    json!({
                        "path": "frame.png",
                        "mime_type": "image/png",
                        "transaction_rid": txn_rid,
                        "sha256": "abababababababababababababababab\
                                   abababababababababababababababab"
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(upload.status(), StatusCode::CREATED);

    // ── 3. Commit succeeds and is idempotent-blocked on retry ──────
    let commit = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/transactions/{txn_rid}/commit"))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(commit.status(), StatusCode::OK);
    let after: Value = serde_json::from_slice(&commit.into_body().collect().await.unwrap().to_bytes()).unwrap();
    assert_eq!(after["state"], "COMMITTED");

    let second_commit = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/transactions/{txn_rid}/commit"))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(second_commit.status(), StatusCode::CONFLICT);
}

#[tokio::test]
async fn aborting_transaction_drops_staged_items() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "abort-set",
        "ri.foundry.main.project.proj-abort",
        TransactionPolicy::Transactional,
    )
    .await;

    let txn = media_sets_service::handlers::transactions::open_transaction_op(
        &h.state, &set.rid, "main", "tester", &common::test_ctx(),
    )
    .await
    .expect("open transaction");

    // Stage an item inside the open transaction.
    let _ = media_sets_service::handlers::items::presigned_upload_op(
        &h.state,
        &set.rid,
        media_sets_service::models::PresignedUploadRequest {
            path: "throwaway.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: Some(txn.rid.clone()),
            sha256: Some("c".repeat(64)),
            size_bytes: Some(123),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage item");

    // Abort.
    media_sets_service::handlers::transactions::close_transaction_op(
        &h.state,
        &txn.rid,
        media_sets_service::models::TransactionState::Aborted,
        &common::test_ctx(),
    )
    .await
    .expect("abort");

    // Items written inside an aborted transaction must be gone.
    let staged: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM media_items WHERE transaction_rid = $1")
        .bind(&txn.rid)
        .fetch_one(&h.pool)
        .await
        .unwrap();
    assert_eq!(staged.0, 0, "aborted transaction should have no surviving items");
}

#[tokio::test]
async fn transactionless_media_set_refuses_open_transaction() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "txless",
        "ri.foundry.main.project.proj-txless",
        TransactionPolicy::Transactionless,
    )
    .await;

    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/media-sets/{}/transactions", set.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(json!({}).to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::CONFLICT);
}
