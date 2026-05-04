//! H4 — Foundry invariant: "Only one transaction can be open on the
//! branch of a media set" (`Advanced media set settings.md` →
//! Transaction policies). The schema enforces this with a partial
//! unique index on `(media_set_rid, branch) WHERE state = 'OPEN'`;
//! this test pins the application-level surface so a future schema
//! refactor that loosens the index would still fail at this gate.

mod common;

use media_sets_service::domain::error::MediaError;
use media_sets_service::handlers::transactions::open_transaction_op;
use media_sets_service::models::{TransactionPolicy, WriteMode};

#[tokio::test]
async fn second_open_on_same_branch_is_rejected_as_terminal() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "concurrent-open-set",
        "ri.foundry.main.project.cct",
        TransactionPolicy::Transactional,
    )
    .await;

    // First open succeeds. Stays OPEN for the rest of the test.
    let first = open_transaction_op(
        &h.state,
        &set.rid,
        "main",
        "tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect("first transaction must open");
    assert_eq!(first.state, "OPEN");

    // Second open on the SAME (set, branch) must be rejected. The
    // partial unique index `uq_media_set_transactions_one_open_per_branch`
    // raises a unique violation that the handler maps to
    // `TransactionTerminal(<set>, "OPEN")` — surfaced as 409 by the
    // REST layer.
    let second = open_transaction_op(
        &h.state,
        &set.rid,
        "main",
        "tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect_err("second open on same (set, branch) must fail");
    match second {
        MediaError::TransactionTerminal(rid, state) => {
            assert_eq!(rid, set.rid, "rejection must reference the offending set");
            assert_eq!(
                state, "OPEN",
                "the existing OPEN transaction is what blocks the second open"
            );
        }
        other => panic!("expected TransactionTerminal(<set>, OPEN), got {other:?}"),
    }
}
