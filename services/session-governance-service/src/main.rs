//! `session-governance-service` binary.
//!
//! Stream **S3.3** of the Cassandra/Foundry parity plan: scoped
//! sessions and restricted-view governance. The substrate
//! ([`policy_postgres`](session_governance_service::policy_postgres) +
//! [`revocation_cassandra`](session_governance_service::revocation_cassandra))
//! is in place; the HTTP handlers land in follow-up PRs. For now
//! this binary boots tracing, listens on the configured port and
//! exposes `/health` so the service mesh can register the pod.

mod config;

use std::net::SocketAddr;

use axum::{Router, routing::get};
use config::AppConfig;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("session_governance_service=info,tower_http=info")
        }))
        .init();

    let config = AppConfig::from_env()?;
    let app = Router::new().route("/health", get(|| async { "ok" }));

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting session-governance-service (substrate-only)");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
