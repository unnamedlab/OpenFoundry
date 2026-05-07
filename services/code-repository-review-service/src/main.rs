//! `code-repository-review-service` binary entry point.
//!
//! S8 / ADR-0030: this binary now owns the cross-plane global-branching
//! HTTP surface absorbed from the retired `global-branch-service`, plus
//! the code-security scan / finding migrations from the retired
//! `code-security-scanning-service`.
//!
//! The Kafka subscriber for `foundry.branch.events.v1` is exposed as
//! the [`code_repository_review_service::global_branch::subscriber::PostgresSubscriber`]
//! port; the binary doesn't pull `rdkafka` directly so this build
//! stays link-time free of the librdkafka native dep. Tests drive
//! the port directly via `SubscriberPort::handle`.

use std::net::SocketAddr;

use code_repository_review_service::{AppConfig, AppState, build_router};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("code_repository_review_service=info,tower_http=info")
        }))
        .init();

    let cfg = AppConfig::from_env()?;
    let pool = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&pool).await?;

    let state = AppState::new(pool.clone(), "system");
    let app = build_router(state);

    let addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!(
        "code-repository-review-service listening on http://{}",
        addr
    );
    axum::serve(listener, app).await?;
    Ok(())
}
