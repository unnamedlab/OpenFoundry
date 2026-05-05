use auth_middleware::jwt::JwtConfig;
use axum::{Router, middleware as axum_mw, routing::any};
use reqwest::Client;

use crate::config::GatewayConfig;
use crate::middleware::rate_limit::RateLimitState;
use crate::proxy::service_router::proxy_handler;

/// Build the /api/v1/* routes that proxy to backend services.
pub fn router(
    config: GatewayConfig,
    client: Client,
    jwt_config: JwtConfig,
    rate_limit_state: RateLimitState,
) -> Router {
    Router::new()
        .route("/api/v1/{*rest}", any(proxy_handler))
        .route("/api/v2/{*rest}", any(proxy_handler))
        // ADR-0041 — surface the Iceberg REST Catalog spec endpoints
        // and the long-lived API-token issuer through the gateway so
        // PyIceberg / Spark / Trino / Snowflake clients can use the
        // same base URL the rest of the platform exposes. Routing to
        // `iceberg-catalog-service` is decided in
        // `proxy::service_router` based on the path prefix.
        .route("/iceberg/v1/{*rest}", any(proxy_handler))
        .route("/v1/iceberg-clients/{*rest}", any(proxy_handler))
        .route_layer(axum_mw::from_fn_with_state(
            rate_limit_state,
            crate::middleware::rate_limit::rate_limit_layer,
        ))
        .with_state((config, client, jwt_config))
}
