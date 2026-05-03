//! Foundry path-deduplication semantics ("Importing media.md"):
//!
//! > If a file is uploaded to a media set and has the same path as an
//! > existing item in the media set, the new item will overwrite the
//! > existing one. […] An overwritten media item will still be available
//! > when using a direct media reference to that media item.
//!
//! Concretely:
//! 1. The previous item gets a `deleted_at` stamp (soft-delete).
//! 2. The new item carries `deduplicated_from = <previous-rid>`.
//! 3. Both items remain individually addressable by RID.
//! 4. The list view (`/media-sets/{rid}/items`) only surfaces the live
//!    one.

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
async fn second_upload_to_same_path_overwrites_and_links_via_deduplicated_from() {
    let h = common::spawn().await;
    let set = common::seed_media_set(
        &h.state,
        "dedup-set",
        "ri.foundry.main.project.proj-dedup",
        TransactionPolicy::Transactionless,
    )
    .await;

    let path = "logo.png";

    // First upload of `logo.png`.
    let first = upload(&h, &set.rid, path, "a".repeat(64).as_str()).await;
    let first_rid = first["item"]["rid"].as_str().unwrap().to_string();

    // Same logical path → triggers dedup.
    let second = upload(&h, &set.rid, path, "b".repeat(64).as_str()).await;
    let second_rid = second["item"]["rid"].as_str().unwrap().to_string();
    assert_ne!(first_rid, second_rid);
    assert_eq!(
        second["item"]["deduplicated_from"].as_str(),
        Some(first_rid.as_str()),
        "new item should reference the previous one via deduplicated_from"
    );

    // The previous row must be soft-deleted in the database.
    let deleted_at: (Option<chrono::DateTime<chrono::Utc>>,) =
        sqlx::query_as("SELECT deleted_at FROM media_items WHERE rid = $1")
            .bind(&first_rid)
            .fetch_one(&h.pool)
            .await
            .unwrap();
    assert!(
        deleted_at.0.is_some(),
        "previous item should be soft-deleted by the dedup pass"
    );

    // Both items remain addressable by RID (Foundry contract).
    for rid in [&first_rid, &second_rid] {
        let resp = h
            .router
            .clone()
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri(format!("/items/{rid}"))
                    .header(AUTHORIZATION, format!("Bearer {}", h.token))
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK, "item {rid} unreachable");
    }

    // Listing only surfaces the live item.
    let list_resp = h
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
    assert_eq!(list_resp.status(), StatusCode::OK);
    let listed: Value = serde_json::from_slice(&list_resp.into_body().collect().await.unwrap().to_bytes()).unwrap();
    let items = listed.as_array().unwrap();
    assert_eq!(items.len(), 1, "list should only return the live item");
    assert_eq!(items[0]["rid"].as_str(), Some(second_rid.as_str()));
}

async fn upload(
    h: &common::Harness,
    set_rid: &str,
    path: &str,
    sha256: &str,
) -> Value {
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/media-sets/{set_rid}/items/upload-url"))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(
                    json!({
                        "path": path,
                        "mime_type": "image/png",
                        "sha256": sha256
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(
        resp.status(),
        StatusCode::CREATED,
        "upload-url for path={path} sha={sha256} failed"
    );
    serde_json::from_slice(&resp.into_body().collect().await.unwrap().to_bytes()).unwrap()
}
