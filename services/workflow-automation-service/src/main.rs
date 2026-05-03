mod config;
mod domain;
mod handlers;
mod models;

use std::{net::SocketAddr, sync::Arc};

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{delete, get, patch, post},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use temporal_client::{Namespace, WorkflowClient};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub http_client: reqwest::Client,
    pub workflow_client: Arc<dyn WorkflowClient>,
    pub temporal_namespace: Namespace,
    pub nats_url: String,
    pub pipeline_service_url: String,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("workflow_automation_service=info,tower_http=info")
            }),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    let http_client = reqwest::Client::new();
    let (workflow_client, temporal_namespace) =
        temporal_client::runtime_workflow_client("workflow-automation-service").await?;

    let state = AppState {
        db,
        http_client,
        workflow_client,
        temporal_namespace,
        nats_url: app_config.nats_url.clone(),
        pipeline_service_url: app_config.pipeline_service_url.clone(),
    };

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();

    let authenticated = Router::new()
        .route(
            "/workflows",
            get(handlers::crud::list_workflows).post(handlers::crud::create_workflow),
        )
        .route(
            "/workflows/{id}",
            get(handlers::crud::get_workflow)
                .patch(handlers::crud::update_workflow)
                .delete(handlers::crud::delete_workflow),
        )
        .route(
            "/workflows/{id}/runs",
            get(handlers::runs::list_runs).post(handlers::execute::start_manual_run),
        )
        .route(
            "/workflows/approvals/{approval_id}/continue",
            post(handlers::approvals::continue_after_approval),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let unauthenticated = Router::new()
        .route(
            "/workflows/{id}/webhook",
            post(handlers::execute::trigger_webhook),
        )
        .route(
            "/workflows/{id}/_internal/lineage",
            post(handlers::execute::start_internal_lineage_run),
        )
        .route(
            "/workflows/{id}/_internal/triggered",
            post(handlers::execute::start_internal_triggered_run),
        );

    let app = Router::new()
        .nest("/api/v1", authenticated.merge(unauthenticated))
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state.clone());

    tokio::spawn(async move {
        if let Err(error) = domain::workflow_run_requested::consume(state).await {
            tracing::error!("workflow run requested consumer stopped: {error}");
        }
    });

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("workflow-automation-service listening on http://{}", addr);
    axum::serve(listener, app).await?;
    Ok(())
}
