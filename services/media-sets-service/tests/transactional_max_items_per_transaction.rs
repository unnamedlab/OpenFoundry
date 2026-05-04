//! H4 — Foundry invariant: "A maximum of 10,000 items can be written
//! in a single transaction" (`Advanced media set settings.md`).
//!
//! Inserting 10,000+1 rows for a real test would dominate runtime
//! (~10s on Postgres testcontainers), so we synthesise the at-cap
//! state with a single bulk INSERT and then assert that the next
//! upload is rejected with the canonical `MEDIA_SET_TRANSACTION_TOO_LARGE`
//! code. The cap constant is read directly from the production module
//! so tests can never drift.

mod common;

use media_sets_service::domain::error::MediaError;
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::handlers::transactions::{
    MAX_ITEMS_PER_TRANSACTION, open_transaction_op,
};
use media_sets_service::models::{PresignedUploadRequest, TransactionPolicy, WriteMode};
use uuid::Uuid;

#[tokio::test]
async fn transactional_upload_rejected_after_ten_thousand_items() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "cap-set",
        "ri.foundry.main.project.cap",
        TransactionPolicy::Transactional,
    )
    .await;

    let txn = open_transaction_op(
        &h.state,
        &set.rid,
        "main",
        "tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect("open transaction");

    // Materialise the at-cap state in one shot. `generate_series` is
    // the cheapest way to build 10k unique paths/SHAs without
    // bottlenecking on per-row roundtrips.
    let inserted: (i64,) = sqlx::query_as(
        r#"
        WITH ins AS (
            INSERT INTO media_items
                (rid, media_set_rid, branch, branch_rid, transaction_rid,
                 path, mime_type, size_bytes, sha256, metadata,
                 storage_uri, retention_seconds)
            SELECT
                'ri.foundry.main.media_item.' || gs::text || '-' || $1,
                $2,
                'main',
                (SELECT branch_rid FROM media_set_branches
                  WHERE media_set_rid = $2 AND branch_name = 'main'),
                $3,
                'cap/' || lpad(gs::text, 6, '0') || '.bin',
                'application/octet-stream',
                1,
                lpad(gs::text, 64, 'f'),
                '{}'::jsonb,
                's3://media/' || gs::text,
                0
            FROM generate_series(1, $4::int) AS gs
            RETURNING 1
        )
        SELECT count(*) FROM ins
        "#,
    )
    .bind(Uuid::now_v7().to_string())
    .bind(&set.rid)
    .bind(&txn.rid)
    .bind(MAX_ITEMS_PER_TRANSACTION as i32)
    .fetch_one(&h.pool)
    .await
    .expect("bulk-seed cap items");
    assert_eq!(inserted.0, MAX_ITEMS_PER_TRANSACTION);

    // The 10,001st upload must trip `MEDIA_SET_TRANSACTION_TOO_LARGE`.
    let err = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "cap/overflow.bin".into(),
            mime_type: "application/octet-stream".into(),
            branch: Some("main".into()),
            transaction_rid: Some(txn.rid.clone()),
            sha256: Some("a".repeat(64)),
            size_bytes: Some(1024),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect_err("upload must be rejected once the cap is hit");

    match err {
        MediaError::TransactionTooLarge(rid, cap) => {
            assert_eq!(rid, txn.rid, "rejection must reference the at-cap transaction");
            assert_eq!(
                cap, MAX_ITEMS_PER_TRANSACTION,
                "rejection must surface the canonical Foundry cap"
            );
        }
        other => panic!("expected TransactionTooLarge, got {other:?}"),
    }
}
