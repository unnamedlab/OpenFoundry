//! A bearer token signed with the iceberg HS256 secret is accepted;
//! one signed with a different secret is rejected with 401.

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use auth_middleware::jwt::JwtConfig;
use iceberg_catalog_service::testing as test_helpers;
use tower::ServiceExt;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn correctly_signed_token_is_accepted() {
    let h = common::spawn().await;
    let token = common::scoped_token(&["api:iceberg-read"]);
    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/config")
                .header("authorization", format!("Bearer {token}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::OK);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn token_signed_with_wrong_secret_is_rejected() {
    let h = common::spawn().await;
    // Issue a token using a different secret. We do this by overriding
    // the static secret-bytes lazy via a fresh process snapshot — the
    // simpler shortcut is to call `jsonwebtoken::encode` with an
    // unrelated key and verify the runtime rejects it.
    let bogus = JwtConfig::new("totally-other-secret")
        .with_issuer("foundry-iceberg")
        .with_audience("iceberg-catalog");
    let token = test_helpers::issue_internal_jwt(
        &bogus,
        "abc",
        "foundry-iceberg",
        "iceberg-catalog",
        &["api:iceberg-read".to_string()],
        3600,
    );
    // This helper signs with the static iceberg secret regardless of
    // the JwtConfig passed (intentional design — the helper consults
    // the same env var as the runtime). We instead manually sign with
    // a foreign key:
    let _ = token;
    use jsonwebtoken::{Algorithm, EncodingKey, Header};
    let claims = serde_json::json!({
        "sub": "abc",
        "iss": "foundry-iceberg",
        "aud": "iceberg-catalog",
        "iat": chrono::Utc::now().timestamp(),
        "exp": chrono::Utc::now().timestamp() + 3600,
        "scp": "api:iceberg-read"
    });
    let foreign = jsonwebtoken::encode(
        &Header::new(Algorithm::HS256),
        &claims,
        &EncodingKey::from_secret(b"unrelated-key"),
    )
    .unwrap();

    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/config")
                .header("authorization", format!("Bearer {foreign}"))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn missing_authorization_header_returns_401() {
    let h = common::spawn().await;
    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .uri("/iceberg/v1/config")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}
