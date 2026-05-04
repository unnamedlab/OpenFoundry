//! H4 — Branch merge resolution policies.
//!
//! Per the H4 spec, `POST /media-sets/{rid}/branches/{name}/merge`
//! ships with two policies:
//!
//!   * `LATEST_WINS` — the source branch's path always wins; the
//!     target's live item at the same path is soft-deleted and the
//!     source's row is copied across.
//!   * `FAIL_ON_CONFLICT` — refuses the merge on the first
//!     overlapping path. The handler emits HTTP 409 with the
//!     conflict surface in the body so callers can switch policies
//!     without re-fetching.
//!
//! The test seeds two transactional branches, deliberately overlaps
//! one path, then runs both merges back-to-back so the outcomes are
//! easy to compare.

mod common;

use media_sets_service::domain::error::MediaError;
use media_sets_service::handlers::branches::{
    create_branch_op, merge_branch_op,
};
use media_sets_service::handlers::items::{list_items_op, presigned_upload_op};
use media_sets_service::handlers::transactions::{
    close_transaction_op, open_transaction_op,
};
use media_sets_service::models::{
    CreateBranchBody, MergeBranchBody, MergeResolution, PresignedUploadRequest,
    TransactionPolicy, TransactionState, WriteMode,
};

async fn upload_in_transaction(
    h: &common::Harness,
    set_rid: &str,
    branch: &str,
    path: &str,
    sha_seed: char,
) -> String {
    let txn = open_transaction_op(
        &h.state,
        set_rid,
        branch,
        "tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect("open transaction");
    let (item, _url) = presigned_upload_op(
        &h.state,
        set_rid,
        PresignedUploadRequest {
            path: path.to_string(),
            mime_type: "application/octet-stream".into(),
            branch: Some(branch.to_string()),
            transaction_rid: Some(txn.rid.clone()),
            sha256: Some(sha_seed.to_string().repeat(64)),
            size_bytes: Some(2048),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("upload");
    close_transaction_op(
        &h.state,
        &txn.rid,
        TransactionState::Committed,
        &common::test_ctx(),
    )
    .await
    .expect("commit");
    item.rid
}

#[tokio::test]
async fn merge_latest_wins_overwrites_target_and_fail_on_conflict_aborts() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "merge-set",
        "ri.foundry.main.project.merge",
        TransactionPolicy::Transactional,
    )
    .await;

    // Mint a `feature` branch off `main`.
    create_branch_op(
        &h.state,
        &set.rid,
        CreateBranchBody {
            name: "feature".into(),
            from_branch: Some("main".into()),
            from_transaction_rid: None,
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("create feature branch");

    // ── Seed shared path on both branches with DIFFERENT bytes ──
    let target_rid = upload_in_transaction(&h, &set.rid, "main", "shared/keep.bin", 'a').await;
    let _source_rid = upload_in_transaction(&h, &set.rid, "feature", "shared/keep.bin", 'b').await;
    // Source-only path; both policies must copy this across.
    let source_only_rid =
        upload_in_transaction(&h, &set.rid, "feature", "feature-only.bin", 'c').await;

    // ── 1. FAIL_ON_CONFLICT must abort with the conflict path ────
    let err = merge_branch_op(
        &h.state,
        &set.rid,
        "feature",
        MergeBranchBody {
            target_branch: "main".into(),
            resolution: MergeResolution::FailOnConflict,
        },
        &common::test_ctx(),
    )
    .await
    .expect_err("FAIL_ON_CONFLICT must reject the merge");
    match err {
        MediaError::MergeConflict(paths) => {
            assert_eq!(paths, vec!["shared/keep.bin"], "conflict surface must list the shared path");
        }
        other => panic!("expected MergeConflict, got {other:?}"),
    }

    // The aborted merge MUST NOT have copied the source-only item:
    // `FAIL_ON_CONFLICT` is all-or-nothing.
    let main_after_fail = list_items_op(&h.state, &set.rid, "main", None, 200, None)
        .await
        .expect("list main after failed merge");
    assert!(
        !main_after_fail.iter().any(|i| i.path == "feature-only.bin"),
        "FAIL_ON_CONFLICT must not write any rows on rejection"
    );
    // The pre-existing target row is still live with its original RID.
    assert!(
        main_after_fail
            .iter()
            .any(|i| i.rid == target_rid && i.path == "shared/keep.bin"),
        "the original target row must still be live after a failed merge"
    );

    // ── 2. LATEST_WINS overwrites the conflict + copies the rest ─
    let resp = merge_branch_op(
        &h.state,
        &set.rid,
        "feature",
        MergeBranchBody {
            target_branch: "main".into(),
            resolution: MergeResolution::LatestWins,
        },
        &common::test_ctx(),
    )
    .await
    .expect("LATEST_WINS must succeed");
    assert_eq!(resp.paths_overwritten, 1, "one path was conflicting");
    assert_eq!(resp.paths_copied, 1, "one path was source-only");
    assert_eq!(resp.paths_skipped, 0);

    // The post-merge view on `main` carries the source's bytes for
    // the shared path AND the source-only item.
    let main_after_win = list_items_op(&h.state, &set.rid, "main", None, 200, None)
        .await
        .expect("list main after winning merge");
    let shared = main_after_win
        .iter()
        .find(|i| i.path == "shared/keep.bin")
        .expect("shared path must remain live on the target");
    assert_ne!(
        shared.rid, target_rid,
        "the original target row must have been soft-deleted and a new row inserted"
    );
    // Source-only item is now reachable on `main` (under a new RID;
    // the merge inserts a fresh row pointing at the source via
    // `deduplicated_from`).
    let copied = main_after_win
        .iter()
        .find(|i| i.path == "feature-only.bin")
        .expect("source-only path must be copied across");
    assert_ne!(copied.rid, source_only_rid);
}
