//! Foundry retention contract — *expansion half*:
//!
//! > When a retention window is expanded, such as from 7 days to 30
//! > days, media items that previously expired (7 days and 1 second)
//! > will not become accessible. The same is true if retention is
//! > changed to "forever".
//!
//! End-to-end:
//! 1. Create a TRANSACTIONLESS set with `retention_seconds = 3600`
//!    (1 hour).
//! 2. Stage an item; backdate `created_at` to 2 hours ago — already
//!    past the window.
//! 3. PATCH retention to itself (no-op increment) so the inline reaper
//!    fires and soft-deletes the item.
//! 4. Expand retention to 30 days, then to "forever" (`0`). The item
//!    must remain `deleted_at IS NOT NULL` through both transitions.

mod common;

use media_sets_service::handlers::items::{get_item_op, presigned_upload_op};
use media_sets_service::handlers::media_sets::{create_media_set_op, patch_retention_op};
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, PresignedUploadRequest, TransactionPolicy,
};

#[tokio::test]
async fn expanding_retention_never_restores_already_expired_items() {
    let h = common::spawn().await;

    // 1. Create with a 1-hour window.
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "growing-set".into(),
            project_rid: "ri.foundry.main.project.grow".into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactionless,
            retention_seconds: 3600,
            virtual_: false,
            source_rid: None,
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("create set");

    // 2. Stage + backdate an item to 2h ago (already out of window).
    let (item, _) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "old.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("b".repeat(64)),
            size_bytes: Some(2048),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage item");
    sqlx::query("UPDATE media_items SET created_at = NOW() - INTERVAL '2 hours' WHERE rid = $1")
        .bind(&item.rid)
        .execute(&h.pool)
        .await
        .unwrap();

    // 3. Trigger the inline reaper.
    patch_retention_op(&h.state, &set.rid, 3600, &common::test_ctx())
        .await
        .expect("reap");
    let after_reap = get_item_op(&h.state, &item.rid).await.expect("get item");
    let first_deleted_at = after_reap
        .deleted_at
        .expect("item should be soft-deleted after the inline reaper");

    // 4. Expand to 30 days. Item MUST stay deleted, with the same
    //    `deleted_at` timestamp (no second flip).
    patch_retention_op(&h.state, &set.rid, 30 * 86_400, &common::test_ctx())
        .await
        .expect("expand to 30d");
    let after_expand = get_item_op(&h.state, &item.rid)
        .await
        .expect("get after expand");
    let still_deleted = after_expand
        .deleted_at
        .expect("expansion must not restore expired items");
    assert_eq!(
        still_deleted, first_deleted_at,
        "deleted_at must be permanent — the schema only allows NULL → NOW(), not the reverse"
    );

    // Switch to "forever" (`0`). Same invariant.
    patch_retention_op(&h.state, &set.rid, 0, &common::test_ctx())
        .await
        .expect("forever");
    let after_forever = get_item_op(&h.state, &item.rid)
        .await
        .expect("get after forever");
    assert_eq!(
        after_forever.deleted_at,
        Some(first_deleted_at),
        "switching to forever must not resurrect previously-expired items"
    );
}
