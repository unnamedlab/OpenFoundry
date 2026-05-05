//! `POST /iceberg/v1/oauth/tokens` with grant_type=client_credentials
//! returns a JWT bearer payload that conforms to the spec.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::Value;
use tower::ServiceExt;
use wiremock::matchers::method;
use wiremock::{Mock, ResponseTemplate};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn client_credentials_grant_returns_jwt_access_token() {
    let h = common::spawn().await;

    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"valid": true})))
        .mount(&h.mock)
        .await;

    let body = "grant_type=client_credentials&client_id=app-1&client_secret=s3cret&scope=api%3Aiceberg-read+api%3Aiceberg-write";

    let response = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/iceberg/v1/oauth/tokens")
                .header(
                    "content-type",
                    "application/x-www-form-urlencoded",
                )
                .body(Body::from(body))
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let bytes = to_bytes(response.into_body(), 1 << 20).await.unwrap();
    let json: Value = serde_json::from_slice(&bytes).unwrap();

    assert_eq!(json["token_type"], "bearer");
    assert_eq!(
        json["issued_token_type"],
        "urn:ietf:params:oauth:token-type:access_token"
    );
    assert!(json["expires_in"].as_i64().unwrap() > 0);
    assert!(json["access_token"].as_str().unwrap().split('.').count() == 3);
    assert!(
        json["scope"]
            .as_str()
            .unwrap()
            .contains("api:iceberg-read")
    );
}
