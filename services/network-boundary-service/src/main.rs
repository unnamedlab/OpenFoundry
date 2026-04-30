//! `network-boundary-service` binary entry point.
//!
//! Owns ingress/egress, private link and proxy boundary policies. Exposes the
//! egress endpoints consumed by the Data Connection app
//! (`/api/v1/data-connection/egress-policies`) plus the broader policy
//! surface under `/api/v1/network-boundary/*`.

mod config;
mod domain;
mod handlers;
mod models;

use std::net::SocketAddr;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, post},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("network_boundary_service=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();
    let state = AppState { db, jwt_config };

    // Data Connection slice expects egress policies at this path.
    let data_connection = Router::new().route(
        "/egress-policies",
        get(handlers::boundary::list_egress_policies).post(handlers::boundary::create_policy),
    );

    // Full network-boundary surface (used by gateway/admin tooling).
    let network_boundary = Router::new()
        .route("/policies", get(handlers::boundary::list_policies).post(handlers::boundary::create_policy))
        .route(
            "/policies/ingress",
            get(handlers::boundary::list_ingress_policies),
        )
        .route(
            "/policies/egress",
            get(handlers::boundary::list_egress_policies),
        )
        .route(
            "/policies/egress/validate",
            post(handlers::boundary::validate_egress),
        )
        .route(
            "/private-links",
            get(handlers::boundary::list_private_links).post(handlers::boundary::create_private_link),
        )
        .route(
            "/proxies",
            get(handlers::boundary::list_proxies).post(handlers::boundary::create_proxy),
        );

    let app = Router::new()
        .nest(
            "/api/v1",
            Router::new()
                .nest("/data-connection", data_connection)
                // Plural form is what edge-gateway-service forwards; singular kept as alias.
                .nest("/network-boundaries", network_boundary.clone())
                .nest("/network-boundary", network_boundary),
        )
        .route("/health", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting network-boundary-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
