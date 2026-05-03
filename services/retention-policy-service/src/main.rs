//! `retention-policy-service` binary.
//!
//! P4 — first non-stub `main`. Wires:
//!   * `AppConfig::from_env()` (reads `DATABASE_URL` / `JWT_SECRET` plus
//!     the `config/{default,prod}.toml` files).
//!   * `sqlx` Postgres pool with the workspace-wide migration set.
//!   * The HTTP `Router` from [`retention_policy_service::build_router`]:
//!     CRUD, applicable-policies, retention-preview, jobs, plus public
//!     `/healthz` and `/metrics`.
//!
//! Mirrors `network-boundary-service::main` so devs jumping between
//! services see the same boot sequence.

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use retention_policy_service::{AppState, build_router, config::AppConfig};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("retention_policy_service=info,tower_http=info")
            }),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = Arc::new(JwtConfig::new(&app_config.jwt_secret).with_env_defaults());
    let state = AppState { db, jwt_config };
    let app = build_router(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting retention-policy-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
