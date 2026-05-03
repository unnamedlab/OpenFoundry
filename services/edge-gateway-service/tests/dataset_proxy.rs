use auth_middleware::jwt::JwtConfig;
use axum::{Router, body::Body, http::StatusCode, routing::any};
use edge_gateway_service::{config::GatewayConfig, proxy::service_router::proxy_handler};
use reqwest::Client;
use serde_json::json;
use tower::ServiceExt;
use uuid::Uuid;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{method, path, query_param},
};

fn gateway_config(catalog_url: &str, versioning_url: &str) -> GatewayConfig {
    let source = format!(
        r#"
            jwt_secret = "test-secret"
            data_asset_catalog_service_url = "{catalog_url}"
            dataset_versioning_service_url = "{versioning_url}"
            dataset_quality_service_url = "{catalog_url}"
        "#
    );
    ::config::Config::builder()
        .add_source(::config::File::from_str(
            &source,
            ::config::FileFormat::Toml,
        ))
        .build()
        .expect("gateway test config")
        .try_deserialize::<GatewayConfig>()
        .expect("gateway config deserialize")
}

fn test_router(config: GatewayConfig) -> Router {
    Router::new()
        .route("/api/v1/{*rest}", any(proxy_handler))
        .with_state((config, Client::new(), JwtConfig::new("test-secret")))
}

async fn request(router: &Router, method: axum::http::Method, uri: String) -> StatusCode {
    let req = axum::http::Request::builder()
        .method(method)
        .uri(uri)
        .header("content-type", "application/json")
        .body(Body::from("{}"))
        .expect("request");
    router.clone().oneshot(req).await.expect("proxy").status()
}

async fn expect_mock(mock: &MockServer, method_name: &str, upstream_path: String) {
    Mock::given(method(method_name))
        .and(path(upstream_path))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({ "ok": true })))
        .mount(mock)
        .await;
}

#[tokio::test]
async fn datasets_ui_routes_proxy_to_catalog_v1_endpoints() {
    let catalog = MockServer::start().await;
    let versioning = MockServer::start().await;
    let router = test_router(gateway_config(&catalog.uri(), &versioning.uri()));
    let id = Uuid::nil();

    expect_mock(&catalog, "GET", "/v1/datasets".to_string()).await;
    expect_mock(&catalog, "POST", "/v1/datasets".to_string()).await;
    expect_mock(&catalog, "GET", format!("/v1/datasets/{id}")).await;
    expect_mock(&catalog, "PATCH", format!("/v1/datasets/{id}")).await;
    expect_mock(&catalog, "DELETE", format!("/v1/datasets/{id}")).await;
    expect_mock(&catalog, "GET", format!("/v1/datasets/{id}/preview")).await;
    expect_mock(&catalog, "GET", format!("/v1/datasets/{id}/schema")).await;
    expect_mock(&catalog, "POST", format!("/v1/datasets/{id}/upload")).await;
    // P3 — `/files` moved to dataset-versioning-service. The legacy
    // `/filesystem` rewrite path is covered by the second test.
    expect_mock(&catalog, "GET", "/v1/catalog/facets".to_string()).await;

    let checks = vec![
        (
            axum::http::Method::GET,
            "/api/v1/datasets?page=1".to_string(),
        ),
        (axum::http::Method::POST, "/api/v1/datasets".to_string()),
        (axum::http::Method::GET, format!("/api/v1/datasets/{id}")),
        (axum::http::Method::PATCH, format!("/api/v1/datasets/{id}")),
        (axum::http::Method::DELETE, format!("/api/v1/datasets/{id}")),
        (
            axum::http::Method::GET,
            format!("/api/v1/datasets/{id}/preview?limit=25"),
        ),
        (
            axum::http::Method::GET,
            format!("/api/v1/datasets/{id}/schema"),
        ),
        (
            axum::http::Method::POST,
            format!("/api/v1/datasets/{id}/upload"),
        ),
        (
            axum::http::Method::GET,
            "/api/v1/datasets/catalog/facets".to_string(),
        ),
    ];

    for (method, uri) in checks {
        assert_eq!(
            request(&router, method, uri.clone()).await,
            StatusCode::OK,
            "{uri} should proxy successfully"
        );
    }
}

#[tokio::test]
async fn dataset_filesystem_alias_and_versioning_routes_keep_compatibility() {
    let catalog = MockServer::start().await;
    let versioning = MockServer::start().await;
    let router = test_router(gateway_config(&catalog.uri(), &versioning.uri()));
    let id = Uuid::nil();

    // Catalog still receives the legacy `/files` listing through the
    // `/filesystem` rewrite. The P3 top-level `/files` endpoint is
    // mocked on the versioning service below.
    Mock::given(method("GET"))
        .and(path(format!("/v1/datasets/{id}/files")))
        .and(query_param("path", "current"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({ "entries": [] })))
        .mount(&catalog)
        .await;
    expect_mock(&versioning, "GET", format!("/v1/datasets/{id}/versions")).await;
    expect_mock(
        &versioning,
        "GET",
        format!("/v1/datasets/{id}/transactions"),
    )
    .await;
    expect_mock(&versioning, "GET", format!("/v1/datasets/{id}/files")).await;

    assert_eq!(
        request(
            &router,
            axum::http::Method::GET,
            format!("/api/v1/datasets/{id}/filesystem?path=current"),
        )
        .await,
        StatusCode::OK
    );
    assert_eq!(
        request(
            &router,
            axum::http::Method::GET,
            format!("/api/v1/datasets/{id}/versions"),
        )
        .await,
        StatusCode::OK
    );
    assert_eq!(
        request(
            &router,
            axum::http::Method::GET,
            format!("/api/v1/datasets/{id}/transactions"),
        )
        .await,
        StatusCode::OK
    );
    assert_eq!(
        request(
            &router,
            axum::http::Method::GET,
            format!("/api/v1/datasets/{id}/files?branch=master"),
        )
        .await,
        StatusCode::OK
    );
}
