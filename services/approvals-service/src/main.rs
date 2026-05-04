//! `approvals-service` binary.
//!
//! Boots the FASE 7 / Tarea 7.3 state-machine runtime: HTTP
//! handlers persist the `audit_compliance.approval_requests` row +
//! the matching `approval.*.v1` outbox event in a single Postgres
//! transaction. The companion CronJob (Tarea 7.4) handles the
//! `pending → expired` transition for rows past their deadline.

mod config;
mod domain;
mod event;
mod handlers;
mod models;
mod topics;

use std::{net::SocketAddr, time::Duration};

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, post},
};
use config::AppConfig;
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub workflow_service_url: String,
    pub ontology_service_url: String,
    pub audit_compliance_service_url: String,
    pub audit_compliance_bearer_token: Option<String>,
    pub approval_ttl_hours: u32,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("approvals_service=info,tower_http=info")),
        )
        .init();

    let config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .acquire_timeout(Duration::from_secs(10))
        .connect(&config.database_url)
        .await?;

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();
    let state = AppState {
        db,
        http_client: reqwest::Client::builder()
            .timeout(Duration::from_secs(30))
            .build()?,
        jwt_config: jwt_config.clone(),
        workflow_service_url: config.workflow_service_url.clone(),
        ontology_service_url: config.ontology_service_url.clone(),
        audit_compliance_service_url: config.audit_compliance_service_url.clone(),
        audit_compliance_bearer_token: config.audit_compliance_bearer_token.clone(),
        approval_ttl_hours: config.approval_ttl_hours,
    };

    let protected = Router::new()
        .route(
            "/approvals",
            get(handlers::approvals::list_approvals).post(handlers::approvals::create_approval),
        )
        .route(
            "/approvals/{approval_id}/decide",
            post(handlers::approvals::decide_approval),
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
    tracing::info!(%addr, "starting approvals-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
