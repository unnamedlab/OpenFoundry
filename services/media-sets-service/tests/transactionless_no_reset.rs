//! H4 — Foundry invariant: "Transactionless media set branches cannot
//! be reset to an empty view." (`Advanced media set settings.md`).
//!
//! The reset op rejects with the canonical
//! `MEDIA_SET_TRANSACTIONLESS_REJECTS_RESET` code (HTTP 422); the
//! companion check is that the same op succeeds against a
//! transactional set (so the rejection is policy-driven, not a bug
//! that locks every set out of reset).

mod common;

use media_sets_service::domain::error::MediaError;
use media_sets_service::handlers::branches::reset_branch_op;
use media_sets_service::models::TransactionPolicy;

#[tokio::test]
async fn transactionless_branch_reset_is_rejected_with_422_code() {
    let h = common::spawn().await;

    // ── 1. TRANSACTIONLESS — reset must be rejected ──────────────
    let tless = common::seed_media_set(
        &h.state,
        "tless-set",
        "ri.foundry.main.project.tless",
        TransactionPolicy::Transactionless,
    )
    .await;
    let err = reset_branch_op(&h.state, &tless.rid, "main", &common::test_ctx())
        .await
        .expect_err("reset must be rejected on transactionless sets");
    match err {
        MediaError::TransactionlessRejectsReset(rid) => {
            assert_eq!(rid, tless.rid, "rejection must reference the offending set");
        }
        other => panic!("expected TransactionlessRejectsReset, got {other:?}"),
    }

    // ── 2. TRANSACTIONAL — same call succeeds ────────────────────
    // No transactions / items have landed yet, so the reset is a
    // no-op (zero rows soft-deleted) — but it must still be allowed
    // so the rejection above is *policy-driven*, not a defect.
    let txal = common::seed_media_set(
        &h.state,
        "txal-set",
        "ri.foundry.main.project.txal",
        TransactionPolicy::Transactional,
    )
    .await;
    let resp = reset_branch_op(&h.state, &txal.rid, "main", &common::test_ctx())
        .await
        .expect("reset must succeed on transactional sets");
    assert_eq!(
        resp.items_soft_deleted, 0,
        "fresh transactional branch has nothing to soft-delete on reset"
    );
    assert!(
        resp.branch.head_transaction_rid.is_none(),
        "reset must rewind the head pointer"
    );
}
