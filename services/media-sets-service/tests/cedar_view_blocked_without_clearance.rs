//! H3 — Cedar denial when the caller is missing a clearance the
//! media set requires.
//!
//! Mirrors the Foundry "media set sensitivity" check: even a project
//! viewer should not be able to *view* a set tagged with markings
//! their JWT does not carry. The 403 must surface the offending
//! marking name verbatim ("missing clearance: SECRET") so the UI can
//! point the operator at the right access-request flow.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use media_sets_service::handlers::media_sets::{create_media_set_op, patch_markings_op};
use media_sets_service::models::{CreateMediaSetRequest, MediaSetSchema, TransactionPolicy};
use tower::ServiceExt;

#[tokio::test]
async fn get_media_set_returns_403_when_caller_lacks_clearance() {
    let h = common::spawn().await;

    // Seed a set marked SECRET. We use the operation layer here so we
    // bypass the harness admin-token shortcut and create the row with
    // the markings the test cares about.
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "classified".into(),
            project_rid: "ri.foundry.main.project.classified".into(),
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
    // Apply markings via the dedicated PATCH op so we exercise the
    // actual write path (the create endpoint accepts markings too,
    // but PATCH is the H3 contract for changing them in place).
    let set = patch_markings_op(
        &h.state,
        &set.rid,
        vec![],
        vec!["secret".into()],
        &common::test_ctx(),
    )
    .await
    .expect("apply markings");

    // Caller token: viewer role + only `public` clearance. They lack
    // `secret`, which is what the Cedar policy requires.
    let viewer_token = common::mint_token(
        &h.jwt_config,
        vec!["viewer".into()],
        vec!["public".into()],
        Some(h.tenant),
    );

    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/media-sets/{}", set.rid))
                .header(AUTHORIZATION, format!("Bearer {viewer_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(resp.status(), StatusCode::FORBIDDEN);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let payload: serde_json::Value = serde_json::from_slice(&body).unwrap();
    let msg = payload["error"].as_str().unwrap_or("");
    assert!(
        msg.to_uppercase().contains("SECRET"),
        "denial body should name the missing marking, got: {msg}"
    );

    // Sanity: an admin with full clearances on the same tenant
    // succeeds — proves the failure mode is the missing clearance,
    // not e.g. tenant isolation.
    let admin_token = common::mint_token(
        &h.jwt_config,
        vec!["admin".into()],
        vec!["public".into(), "secret".into()],
        Some(h.tenant),
    );
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/media-sets/{}", set.rid))
                .header(AUTHORIZATION, format!("Bearer {admin_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
}
