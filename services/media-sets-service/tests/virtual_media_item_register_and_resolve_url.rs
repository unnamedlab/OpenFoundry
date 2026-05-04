//! Foundry virtual media set contract:
//!
//! 1. Items can be registered without copying bytes
//!    (`POST /media-sets/{rid}/virtual-items`).
//! 2. `GET /items/{rid}/download-url` for a virtual item resolves the
//!    source endpoint via `connector-management-service` and returns a
//!    URL that points at the **external** system, not at Foundry
//!    storage.
//! 3. When `connector-management-service` is not configured, the
//!    handler returns HTTP 503 with a clear error rather than fabricate
//!    a URL.

mod common;

use axum::{
    body::Body,
    http::{Request, StatusCode, header::AUTHORIZATION},
};
use http_body_util::BodyExt;
use media_sets_service::handlers::media_sets::create_media_set_op;
use media_sets_service::models::{CreateMediaSetRequest, MediaSetSchema, TransactionPolicy};
use serde_json::{Value, json};
use tower::ServiceExt;
use wiremock::matchers::{method, path_regex};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test]
async fn register_virtual_item_then_download_url_resolves_via_connector_service() {
    let connector_mock = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path_regex(r"^/sources/.+$"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "rid": "ri.foundry.main.source.fake-001",
            "endpoint": "https://external.example.com/bucket-x",
            "kind": "s3"
        })))
        .mount(&connector_mock)
        .await;

    let h = common::spawn_with_connector(Some(connector_mock.uri())).await;

    // Create a virtual media set bound to the mocked source.
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "external-screenshots".into(),
            project_rid: "ri.foundry.main.project.virtual".into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactionless,
            retention_seconds: 0,
            virtual_: true,
            source_rid: Some("ri.foundry.main.source.fake-001".into()),
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("create virtual set");

    // Register an item that lives in the external system.
    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/media-sets/{}/virtual-items", set.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .header("content-type", "application/json")
                .body(Body::from(
                    json!({
                        "physical_path": "s3://external-bucket/path/to/image.png",
                        "item_path": "screens/login.png",
                        "mime_type": "image/png"
                    })
                    .to_string(),
                ))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::CREATED);
    let item: Value =
        serde_json::from_slice(&resp.into_body().collect().await.unwrap().to_bytes()).unwrap();
    let item_rid = item["rid"].as_str().unwrap().to_string();
    assert_eq!(
        item["storage_uri"],
        "s3://external-bucket/path/to/image.png"
    );

    // Resolve the download URL — it must point at the mocked external
    // endpoint, NOT at Foundry's local storage backend.
    let download = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{item_rid}/download-url"))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(download.status(), StatusCode::OK);
    let body: Value =
        serde_json::from_slice(&download.into_body().collect().await.unwrap().to_bytes()).unwrap();
    let url = body["url"].as_str().unwrap();
    assert!(
        url.starts_with("https://external.example.com/bucket-x/"),
        "virtual download URL should point at the external endpoint, got: {url}"
    );
    assert!(
        url.contains("path/to/image.png"),
        "virtual download URL must preserve the source path, got: {url}"
    );
    assert!(
        !url.starts_with("local://"),
        "virtual download URL must not fall back to the Foundry backend, got: {url}"
    );
}

#[tokio::test]
async fn virtual_download_returns_503_when_connector_service_is_unconfigured() {
    let h = common::spawn_with_connector(None).await;

    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "no-connector-set".into(),
            project_rid: "ri.foundry.main.project.no-conn".into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactionless,
            retention_seconds: 0,
            virtual_: true,
            source_rid: Some("ri.foundry.main.source.fake-002".into()),
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("create virtual set");

    // Register the item directly so we have something to ask a URL for.
    let item = media_sets_service::handlers::items::register_virtual_item_op(
        &h.state,
        &set.rid,
        media_sets_service::handlers::items::RegisterVirtualItemRequest {
            physical_path: "s3://other/external.png".into(),
            item_path: "external.png".into(),
            mime_type: Some("image/png".into()),
            size_bytes: None,
            branch: None,
            sha256: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("register virtual item");

    let download = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{}/download-url", item.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(
        download.status(),
        StatusCode::SERVICE_UNAVAILABLE,
        "without connector wiring the handler must return 503 rather than fabricate a URL"
    );
}
