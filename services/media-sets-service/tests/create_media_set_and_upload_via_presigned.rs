//! End-to-end: create a media set via REST and obtain a presigned upload
//! URL for an item under it. Verifies that the persisted MediaItem row
//! lands with the expected storage_uri layout.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use serde_json::{Value, json};
use tower::ServiceExt;

#[tokio::test]
async fn create_media_set_then_upload_url_returns_presigned_link_and_persists_item() {
    let h = common::spawn().await;

    // ── 1. Create the media set via POST /media-sets ──────────────
    let create_resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/media-sets")
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(
                    json!({
                        "name": "screenshots",
                        "project_rid": "ri.foundry.main.project.proj-1",
                        "schema": "IMAGE",
                        "allowed_mime_types": ["image/png"],
                        "transaction_policy": "TRANSACTIONLESS"
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(create_resp.status(), StatusCode::CREATED);
    let body = create_resp.into_body().collect().await.unwrap().to_bytes();
    let set: Value = serde_json::from_slice(&body).unwrap();
    let set_rid = set["rid"].as_str().unwrap().to_string();
    assert!(set_rid.starts_with("ri.foundry.main.media_set."));
    assert_eq!(set["transaction_policy"], "TRANSACTIONLESS");

    // ── 2. Request a presigned upload URL ──────────────────────────
    let upload_resp = h
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
                        "path": "screens/login.png",
                        "mime_type": "image/png",
                        "sha256": "11111111111111111111111111111111\
                                   11111111111111111111111111111111"
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(upload_resp.status(), StatusCode::CREATED);
    let body = upload_resp.into_body().collect().await.unwrap().to_bytes();
    let payload: Value = serde_json::from_slice(&body).unwrap();

    let url = payload["url"].as_str().unwrap();
    assert!(url.starts_with("local://media/"), "got url: {url}");
    assert!(
        url.contains(&format!("media-sets/{set_rid}/main/11/11111111")),
        "url did not include sha-sharded path: {url}"
    );

    let item = &payload["item"];
    let item_rid = item["rid"].as_str().unwrap();
    assert!(item_rid.starts_with("ri.foundry.main.media_item."));
    assert_eq!(item["path"], "screens/login.png");
    assert_eq!(item["branch"], "main");
    assert_eq!(item["mime_type"], "image/png");

    // ── 3. The row should be queryable via GET /items/{rid} ────────
    let get_resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{item_rid}"))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(get_resp.status(), StatusCode::OK);
}
