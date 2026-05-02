//! HTTP mocks for stubbing neighbour services in integration tests.
//!
//! Wraps [`wiremock::MockServer`] with sane defaults so each test can
//! spin up a fresh ephemeral server and obtain its `base_url` to inject
//! into the service AppState (e.g. `data_asset_catalog_url`,
//! `retention_policy_url`, lineage client base, audit sink).

use serde_json::Value;
use wiremock::matchers::{any, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

/// Start a fresh `wiremock` server on an ephemeral port and return it
/// together with its base URL (no trailing slash).
pub async fn start_neighbor() -> (MockServer, String) {
    let server = MockServer::start().await;
    let base = server.uri();
    (server, base)
}

/// Convenience: install a catch-all `200 {}` mock so the service under
/// test never sees a 404 from the neighbour during tests that don't
/// care about the call shape.
pub async fn install_default_ok(server: &MockServer) {
    Mock::given(any())
        .respond_with(ResponseTemplate::new(200).set_body_json(Value::Object(Default::default())))
        .mount(server)
        .await;
}

/// Install a typed JSON response on a specific `METHOD path` pair.
pub async fn install_json(server: &MockServer, http_method: &str, route: &str, body: Value) {
    Mock::given(method(http_method))
        .and(path(route))
        .respond_with(ResponseTemplate::new(200).set_body_json(body))
        .mount(server)
        .await;
}

