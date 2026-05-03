//! `automation-operations-service` binary.
//!
//! HTTP boot for the Temporal-backed automation-ops facade. New
//! writes route through
//! [`automation_operations_service::domain::temporal_adapter`]
//! (Stream S2.7 of the Cassandra/Foundry parity plan); list/get/run
//! endpoints expose only non-authoritative operational projections.

use std::net::SocketAddr;

use auth_middleware::jwt::JwtConfig;
use automation_operations_service::{
    AppState,
    config::AppConfig,
    domain::temporal_adapter::AutomationOpsAdapter,
    handlers::{create_item, create_secondary, get_item, list_items, list_secondary},
};
use axum::{Router, routing::get};
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("automation_operations_service=info,tower_http=info")
        }))
        .init();

    let config = AppConfig::from_env()?;
    if config.database_url.is_some() {
        tracing::warn!(
            "DATABASE_URL is ignored by automation-operations-service runtime; Temporal is authoritative"
        );
    }

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();
    let state = AppState {
        adapter: AutomationOpsAdapter::from_env("automation-operations-service").await?,
    };

    let protected = Router::new()
        .route("/automations", get(list_items).post(create_item))
        .route("/automations/:id", get(get_item))
        .route(
            "/automations/:parent_id/runs",
            get(list_secondary).post(create_secondary),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1", protected)
        .route("/health", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting automation-operations-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
