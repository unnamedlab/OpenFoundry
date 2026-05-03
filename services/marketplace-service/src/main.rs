//! `marketplace-service` binary.
//!
//! P5 — first non-stub `main`. Mirrors the other workspace services:
//! parses [`config::AppConfig`], opens an SQLx Postgres pool, applies
//! the marketplace migration set, and serves
//! [`marketplace_service::build_router`].

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use marketplace_service::{AppState, build_router, config::AppConfig};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("marketplace_service=info,tower_http=info")),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = Arc::new(JwtConfig::new(&app_config.jwt_secret).with_env_defaults());
    let state = AppState {
        db,
        jwt_config,
        http_client: reqwest::Client::new(),
        app_builder_service_url: app_config.app_builder_service_url.clone(),
    };
    let app = build_router(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting marketplace-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
