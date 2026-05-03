//! H3 — Per-item granular override.
//!
//! Foundry contract ("Configure granular policies for media items.md"):
//! when an individual item carries a stricter marking than its parent
//! set, only operators cleared for that marking should see the item.
//!
//! Setup:
//!   * Parent media set marked `pii`.
//!   * Two items: `public.png` (no override) and `top-secret.png`
//!     (override `secret`).
//!
//! Caller has clearances `[pii]` only — they should see the parent
//! set, list `public.png`, fail to fetch `top-secret.png` directly.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use media_sets_service::handlers::items::{
    patch_item_markings_op, presigned_upload_op,
};
use media_sets_service::handlers::media_sets::{
    create_media_set_op, patch_markings_op,
};
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, PresignedUploadRequest, TransactionPolicy,
};
use serde_json::Value;
use tower::ServiceExt;

#[tokio::test]
async fn item_marked_secret_is_hidden_when_caller_only_has_pii_clearance() {
    let h = common::spawn().await;

    // ── Seed: PII set + two items ─────────────────────────────────
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "evidence".into(),
            project_rid: "ri.foundry.main.project.evidence".into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactionless,
            retention_seconds: 0,
            virtual_: false,
            source_rid: None,
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("seed set");
    let set = patch_markings_op(
        &h.state,
        &set.rid,
        vec![],
        vec!["pii".into()],
        &common::test_ctx(),
    )
    .await
    .expect("apply set markings");

    let (public_item, _) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "public.png".into(),
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
    .expect("upload public item");

    let (secret_item, _) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "top-secret.png".into(),
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
    .expect("upload secret item");

    // Tighten the second item with a per-item SECRET override.
    patch_item_markings_op(
        &h.state,
        &secret_item.rid,
        vec![],
        vec!["secret".into()],
        &set,
        &common::test_ctx(),
    )
    .await
    .expect("apply per-item marking");

    // Caller: PII clearance only.
    let pii_token = common::mint_token(
        &h.jwt_config,
        vec!["viewer".into()],
        vec!["pii".into()],
        Some(h.tenant),
    );

    // ── 1. Listing surfaces only the public item ──────────────────
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/media-sets/{}/items?branch=main", set.rid))
                .header(AUTHORIZATION, format!("Bearer {pii_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let listed: Value = serde_json::from_slice(&body).unwrap();
    let items = listed.as_array().unwrap();
    assert_eq!(items.len(), 1, "expected granular override to hide secret item");
    assert_eq!(items[0]["rid"].as_str(), Some(public_item.rid.as_str()));

    // ── 2. Direct fetch of the SECRET item is forbidden ───────────
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{}", secret_item.rid))
                .header(AUTHORIZATION, format!("Bearer {pii_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::FORBIDDEN);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let payload: Value = serde_json::from_slice(&body).unwrap();
    assert!(
        payload["error"]
            .as_str()
            .unwrap_or("")
            .to_uppercase()
            .contains("SECRET"),
        "denial body should mention the missing SECRET clearance"
    );

    // ── 3. Public sibling stays accessible to the same caller ─────
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{}", public_item.rid))
                .header(AUTHORIZATION, format!("Bearer {pii_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    // ── 4. A SECRET-cleared caller sees both items ────────────────
    let secret_token = common::mint_token(
        &h.jwt_config,
        vec!["viewer".into()],
        vec!["pii".into(), "secret".into()],
        Some(h.tenant),
    );
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/media-sets/{}/items?branch=main", set.rid))
                .header(AUTHORIZATION, format!("Bearer {secret_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let listed: Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(
        listed.as_array().unwrap().len(),
        2,
        "fully-cleared caller should see both items"
    );
}
