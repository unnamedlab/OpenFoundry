//! H3 — Presigned download URL is NOT issued when the caller lacks
//! clearance, and the embedded short-lived JWT claim verifies cleanly
//! when it IS issued.
//!
//! Two halves:
//!   1. Without clearance: `GET /items/{rid}/download-url` returns 403
//!      and the response body carries no presigned URL — Foundry
//!      "sin clearance vigente, URL no se emite".
//!   2. With clearance: the URL embeds a `?claim=<jwt>` segment that
//!      `verify_presign_claim` accepts with `exp - iat ≤ 5 min`.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use media_sets_service::domain::cedar::{PRESIGN_CLAIM_DEFAULT_TTL_SECS, verify_presign_claim};
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::handlers::media_sets::{create_media_set_op, patch_markings_op};
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, PresignedUploadRequest, TransactionPolicy,
};
use serde_json::Value;
use tower::ServiceExt;
use url::Url;

#[tokio::test]
async fn download_url_blocked_without_clearance_and_carries_short_lived_claim_otherwise() {
    let h = common::spawn().await;

    // Seed a SECRET-marked set + one item.
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "vault".into(),
            project_rid: "ri.foundry.main.project.vault".into(),
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
        vec!["secret".into()],
        &common::test_ctx(),
    )
    .await
    .expect("apply markings");
    let (item, _) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "vault/key.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("c".repeat(64)),
            size_bytes: Some(4096),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("upload item");

    // ── 1. Caller without SECRET clearance: 403, no URL leaks ─────
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
                .uri(format!("/items/{}/download-url", item.rid))
                .header(AUTHORIZATION, format!("Bearer {viewer_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::FORBIDDEN);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let payload: Value = serde_json::from_slice(&body).unwrap();
    let body_str = serde_json::to_string(&payload).unwrap();
    assert!(
        !body_str.contains("local://") && !body_str.contains("https://"),
        "denial body must not leak the presigned URL: {body_str}"
    );

    // ── 2. Caller with SECRET clearance: URL minted + claim parses ─
    let secret_token = common::mint_token(
        &h.jwt_config,
        vec!["editor".into()],
        vec!["secret".into()],
        Some(h.tenant),
    );
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{}/download-url", item.rid))
                .header(AUTHORIZATION, format!("Bearer {secret_token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let payload: Value = serde_json::from_slice(&body).unwrap();
    let url = payload["url"].as_str().expect("url").to_string();
    let parsed = Url::parse(&url).expect("parse presigned url");
    let claim_token = parsed
        .query_pairs()
        .find(|(k, _)| k == "claim")
        .map(|(_, v)| v.into_owned())
        .expect("presigned URL must embed the ?claim=<jwt> guard token");

    let secret = h.state.presign_secret.as_slice();
    let claim = verify_presign_claim(secret, &claim_token, &item.rid)
        .expect("claim should validate against the same secret the service signed with");
    assert!(
        claim.exp - claim.iat <= PRESIGN_CLAIM_DEFAULT_TTL_SECS,
        "claim TTL must be capped at the 5-minute default — got {}s",
        claim.exp - claim.iat
    );
    assert!(
        claim
            .markings
            .iter()
            .any(|m| m.eq_ignore_ascii_case("secret")),
        "claim must snapshot the item's effective markings: {:?}",
        claim.markings
    );

    // Tampering: a different item RID must fail the gateway-side
    // verification, even with a valid signature.
    let err = verify_presign_claim(secret, &claim_token, "ri.foundry.main.media_item.other")
        .expect_err("rid mismatch must reject");
    let msg = err.to_string();
    assert!(msg.contains("targets"), "{msg}");
}
