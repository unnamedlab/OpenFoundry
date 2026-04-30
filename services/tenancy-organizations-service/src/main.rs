//! `tenancy-organizations-service` binary entry point.
//!
//! Runs migrations against three databases (tenancy / nexus / ontology)
//! and serves the workspace HTTP surface (`/api/v1/workspace/*`) used by
//! the B3 Workspace UI. Existing organization, project, space and
//! enrollment handlers stay scaffolded for now and will be wired in by
//! the dedicated tenancy work item — they are *not* mounted here yet.

mod config;
mod domain;
mod handlers;
mod models;
mod routes;

use std::net::SocketAddr;

use auth_middleware::{jwt::JwtConfig, layer::auth_layer};
use axum::{Router, middleware};
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

/// Shared state injected into every Axum handler.
///
/// Three `PgPool` fields because this service spans three logical
/// databases:
///
/// * `db` — the tenancy database (organizations, enrollments,
///   `user_favorites`, `resource_access_log`, `resource_shares`).
/// * `nexus_db` — federated peers and shared spaces.
/// * `ontology_db` — projects, folders and resource bindings; targeted
///   by the workspace handlers (trash, move, rename, duplicate).
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub nexus_db: PgPool,
    pub ontology_db: PgPool,
    pub jwt_config: JwtConfig,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("tenancy_organizations_service=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;

    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let nexus_db = PgPoolOptions::new()
        .max_connections(5)
        .connect(&app_config.nexus_database_url)
        .await?;

    let ontology_db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.ontology_database_url)
        .await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();

    let state = AppState {
        db,
        nexus_db,
        ontology_db,
        jwt_config: jwt_config.clone(),
    };

    // All workspace routes require an authenticated user; auth_layer
    // populates request extensions with `Claims` for the AuthUser
    // extractor inside each handler.
    let workspace = routes::workspace_router()
        .with_state(state.clone())
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_layer,
        ));

    let app = Router::new().nest("/api/v1/workspace", workspace);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!("tenancy-organizations-service listening on http://{addr}");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;

    Ok(())
}
