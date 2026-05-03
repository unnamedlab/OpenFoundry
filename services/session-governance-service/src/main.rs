//! `session-governance-service` binary — Stream **S3.3** of the
//! Cassandra/Foundry parity plan.
//!
//! Wires the Cassandra-backed revocation substrate
//! ([`session_governance_service::revocation_cassandra::RevocationAdapter`])
//! into an HTTP surface gated by the `admin:session-governor` role
//! (Cedar policy [`policies/session_governor.cedar`](../policies/session_governor.cedar)).
//!
//! The handler-by-handler refactor of scoped-session UX continues per
//! ADR-0024; for now this bin owns the three governance endpoints
//! that close the S3.3 acceptance criteria:
//!
//! * `POST /governance/sessions/{session_id}/revoke`
//! * `POST /governance/users/{user_id}/revoke`
//! * `GET  /governance/sessions/{session_id}/status`
//!
//! Cassandra and JWT settings are sourced from environment variables
//! (`CASSANDRA_CONTACT_POINTS`, `CASSANDRA_LOCAL_DATACENTER`,
//! `CASSANDRA_KEYSPACE`, `JWT_SECRET`/`OPENFOUNDRY_JWT_SECRET`,
//! `JWT_ISSUER`, `JWT_AUDIENCE`, `JWT_KID`, `JWT_*_KEY_PEM`/`_PATH`)
//! the same way as `identity-federation-service`, so a single shared
//! deployment manifest can configure both bins.

mod config;

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, post},
};
use cassandra_kernel::{ClusterConfig, SessionBuilder};
use config::AppConfig;
use session_governance_service::revocation_cassandra::RevocationAdapter;
use tracing_subscriber::EnvFilter;

#[path = "../../identity-federation-service/src/sessions_cassandra.rs"]
mod sessions_cassandra;

#[path = "handlers/common.rs"]
mod common;

#[path = "handlers/revocation.rs"]
mod revocation;

/// Shared application state injected into the Axum router. Cloned
/// per-request; the inner `Arc<scylla::Session>` keeps the Cassandra
/// connection pool a single instance across the binary.
#[derive(Clone)]
pub struct AppState {
    pub jwt_config: JwtConfig,
    pub revocation: RevocationAdapter,
    pub sessions: sessions_cassandra::SessionsAdapter,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("session_governance_service=info,tower_http=info")
            }),
        )
        .init();

    let config = AppConfig::from_env()?;

    let cassandra_session = build_cassandra_session().await?;
    let revocation = RevocationAdapter::new(cassandra_session.clone());
    revocation.migrate().await?;
    let sessions = sessions_cassandra::SessionsAdapter::new(cassandra_session);

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();

    let state = AppState {
        jwt_config: jwt_config.clone(),
        revocation,
        sessions,
    };

    let governance = Router::new()
        .route(
            "/governance/sessions/{session_id}/revoke",
            post(revocation::revoke_session),
        )
        .route(
            "/governance/users/{user_id}/revoke",
            post(revocation::revoke_user_sessions),
        )
        .route(
            "/governance/sessions/{session_id}/status",
            get(revocation::session_status),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .merge(governance)
        .route("/health", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting session-governance-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn build_cassandra_session()
-> Result<Arc<cassandra_kernel::scylla::Session>, Box<dyn std::error::Error>> {
    let contact_points =
        std::env::var("CASSANDRA_CONTACT_POINTS").unwrap_or_else(|_| "127.0.0.1:9042".to_string());
    let datacenter =
        std::env::var("CASSANDRA_LOCAL_DATACENTER").unwrap_or_else(|_| "dc1".to_string());
    let keyspace = std::env::var("CASSANDRA_KEYSPACE").ok();

    let cluster = ClusterConfig {
        contact_points: contact_points
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect(),
        local_datacenter: datacenter,
        keyspace,
        ..ClusterConfig::dev_local()
    };
    Ok(Arc::new(SessionBuilder::new(cluster).build().await?))
}
