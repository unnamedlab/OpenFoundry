//! `approvals-service` binary.
//!
//! Stream **S2.5** of the Cassandra/Foundry parity plan: durable
//! approval state migrates to Temporal (`ApprovalRequestWorkflow`).
//! New writes route through
//! [`approvals_service::domain::temporal_adapter::ApprovalsAdapter`].
//! This binary exposes Temporal-backed request and decision handlers;
//! any SQL-backed read model is non-authoritative projection only.

mod config;
mod domain;
mod handlers;
mod models;

use std::{net::SocketAddr, sync::Arc};

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, post},
};
use config::AppConfig;
use sqlx::{PgPool, postgres::PgPoolOptions};
use temporal_client::{Namespace, WorkflowClient};
use tracing_subscriber::EnvFilter;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub workflow_service_url: String,
    pub ontology_service_url: String,
    pub workflow_client: Arc<dyn WorkflowClient>,
    pub temporal_namespace: Namespace,
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
        .connect(&config.database_url)
        .await?;

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();
    let (workflow_client, temporal_namespace) =
        temporal_client::runtime_workflow_client("approvals-service").await?;
    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        jwt_config: jwt_config.clone(),
        workflow_service_url: config.workflow_service_url.clone(),
        ontology_service_url: config.ontology_service_url.clone(),
        workflow_client,
        temporal_namespace,
    };

    let protected = Router::new()
        .route(
            "/approvals",
            get(handlers::approvals::list_approvals).post(handlers::approvals::create_approval),
        )
        .route(
            "/approvals/:approval_id/decide",
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
