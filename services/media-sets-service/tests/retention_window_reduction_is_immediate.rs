//! Foundry retention contract — *reduction half*:
//!
//! > When a retention window is reduced, such as from 30 days to 7
//! > days, all media items that are older than the new window (7 days)
//! > will immediately become inaccessible.
//!
//! End-to-end:
//! 1. Create a TRANSACTIONLESS set with `retention_seconds = 86400`
//!    (1 day) and stage an item.
//! 2. Backdate the item's `created_at` to 2 hours ago via raw SQL —
//!    well within the original 1-day window so it stays live.
//! 3. PATCH the set's retention down to 3600 seconds (1 hour). The
//!    handler runs the reaper inline, so the item becomes inaccessible
//!    *before* the response returns — no waiting on the periodic loop.
//! 4. Verify the row's `deleted_at` is now stamped.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::handlers::media_sets::create_media_set_op;
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, PresignedUploadRequest, TransactionPolicy,
};
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test]
async fn patch_retention_down_immediately_expires_in_window_items() {
    let h = common::spawn().await;

    // 1. Set with 1-day retention.
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "shrinking-set".into(),
            project_rid: "ri.foundry.main.project.shrink".into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactionless,
            retention_seconds: 86_400,
            virtual_: false,
            source_rid: None,
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("create set");

    // 2. Stage an item.
    let (item, _) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "screenshot.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("a".repeat(64)),
            size_bytes: Some(1024),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage item");

    // Backdate created_at 2h into the past (well inside the original
    // 1-day window so it is still live at this point).
    sqlx::query("UPDATE media_items SET created_at = NOW() - INTERVAL '2 hours' WHERE rid = $1")
        .bind(&item.rid)
        .execute(&h.pool)
        .await
        .unwrap();
    let still_live: (Option<chrono::DateTime<chrono::Utc>>,) =
        sqlx::query_as("SELECT deleted_at FROM media_items WHERE rid = $1")
            .bind(&item.rid)
            .fetch_one(&h.pool)
            .await
            .unwrap();
    assert!(
        still_live.0.is_none(),
        "item should still be live before PATCH"
    );

    // 3. Reduce retention to 1h. Handler runs the reaper synchronously.
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("PATCH")
                .uri(format!("/media-sets/{}/retention", set.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(json!({ "retention_seconds": 3600 }).to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let updated: Value =
        serde_json::from_slice(&resp.into_body().collect().await.unwrap().to_bytes()).unwrap();
    assert_eq!(updated["retention_seconds"], 3600);

    // 4. Item must have been soft-deleted in-line.
    let after: (Option<chrono::DateTime<chrono::Utc>>,) =
        sqlx::query_as("SELECT deleted_at FROM media_items WHERE rid = $1")
            .bind(&item.rid)
            .fetch_one(&h.pool)
            .await
            .unwrap();
    assert!(
        after.0.is_some(),
        "PATCH retention down must expire previously-in-window items immediately"
    );

    // The list view must no longer surface it.
    let list = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/media-sets/{}/items?branch=main", set.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(list.status(), StatusCode::OK);
    let listed: Value =
        serde_json::from_slice(&list.into_body().collect().await.unwrap().to_bytes()).unwrap();
    assert!(
        listed.as_array().unwrap().is_empty(),
        "expired item must not appear in the live listing"
    );
}
