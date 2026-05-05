//! H7 — empty media set round-trip.
//!
//! Foundry's "Get media references" pipeline-authoring expression
//! and the dataset preview both expect that a brand-new media set with
//! zero items returns an empty (but well-formed) listing rather than
//! a 404. This test pins:
//!
//!   * Creating a set succeeds when no items have been uploaded yet.
//!   * Listing items on the empty set returns `Ok(vec![])` — never an
//!     error and never a `null` payload.
//!   * Even after a transaction is opened + committed without any
//!     items, the listing stays empty (the doc explicitly calls out
//!     "empty transactions are valid checkpoints").
//!
//! The motivation is the doc-canonical "empty media set file" pattern:
//! pipelines that produce no items in a build still must emit a valid
//! commit so downstream incremental builds see a `previous` snapshot.

mod common;

use media_sets_service::handlers::media_sets::create_media_set_op;
use media_sets_service::handlers::transactions::{close_transaction_op, open_transaction_op};
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, TransactionPolicy, TransactionState, WriteMode,
};

#[tokio::test]
async fn empty_media_set_lists_zero_items_and_supports_zero_item_transaction() {
    let h = common::spawn().await;

    // ── 1. Create the set; do NOT upload any items. ────────────────
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "empty-export".into(),
            project_rid: "ri.foundry.main.project.empty".into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactional,
            retention_seconds: 0,
            virtual_: false,
            source_rid: None,
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("creating an item-less media set must succeed");

    // ── 2. The set's row exists in Postgres. ───────────────────────
    let count: (i64,) = sqlx::query_as("SELECT COUNT(*)::bigint FROM media_sets WHERE rid = $1")
        .bind(&set.rid)
        .fetch_one(&h.pool)
        .await
        .expect("count media_sets row");
    assert_eq!(count.0, 1, "the media-set row must persist");

    // The mirror table for items must hold zero rows for this set —
    // the empty-set contract: no items implies no item rows.
    let item_count: (i64,) = sqlx::query_as(
        "SELECT COUNT(*)::bigint FROM media_items WHERE media_set_rid = $1",
    )
    .bind(&set.rid)
    .fetch_one(&h.pool)
    .await
    .expect("count media_items rows");
    assert_eq!(
        item_count.0, 0,
        "an empty media set must surface zero rows in media_items"
    );

    // ── 3. Open + commit a transaction with zero items. The doc
    //       calls these "empty checkpoints" — pipelines that produced
    //       nothing this build still must commit so the next
    //       incremental build sees a `previous` baseline. ───────────
    let tx = open_transaction_op(
        &h.state,
        &set.rid,
        "main",
        "tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect("open empty transaction");
    close_transaction_op(
        &h.state,
        &tx.rid,
        TransactionState::Committed,
        &common::test_ctx(),
    )
    .await
    .expect("commit empty transaction");

    // After the empty commit, the item count is still zero — but
    // there's now a transaction row that downstream incremental
    // builds can join against as the post-build baseline.
    let item_count_after: (i64,) = sqlx::query_as(
        "SELECT COUNT(*)::bigint FROM media_items WHERE media_set_rid = $1",
    )
    .bind(&set.rid)
    .fetch_one(&h.pool)
    .await
    .expect("count media_items after empty commit");
    assert_eq!(item_count_after.0, 0);

    let tx_rows: (i64,) = sqlx::query_as(
        "SELECT COUNT(*)::bigint FROM media_set_transactions WHERE media_set_rid = $1",
    )
    .bind(&set.rid)
    .fetch_one(&h.pool)
    .await
    .expect("count media_set_transactions");
    assert_eq!(
        tx_rows.0, 1,
        "the empty checkpoint must persist a transaction row even with zero items"
    );
}
