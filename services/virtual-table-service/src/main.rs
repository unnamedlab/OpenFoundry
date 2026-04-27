#![allow(dead_code)]

mod config;
mod connectors;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{get, post},
};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;
use std::time::Duration;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub dataset_service_url: String,
    pub pipeline_service_url: String,
    pub ontology_service_url: String,
    pub network_boundary_service_url: String,
    pub allowed_egress_hosts: Vec<String>,
    pub allow_private_network_egress: bool,
    pub agent_stale_after: chrono::Duration,
}

impl axum::extract::FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("virtual-table-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(Duration::from_secs(60))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        dataset_service_url: cfg.dataset_service_url.clone(),
        pipeline_service_url: cfg.pipeline_service_url.clone(),
        ontology_service_url: cfg.ontology_service_url.clone(),
        network_boundary_service_url: cfg.network_boundary_service_url.clone(),
        allowed_egress_hosts: cfg.allowed_egress_hosts.clone(),
        allow_private_network_egress: cfg.allow_private_network_egress,
        agent_stale_after: chrono::Duration::seconds(cfg.agent_stale_after_secs.max(15) as i64),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("virtual-table-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/connections/{id}/discover",
            post(handlers::registrations::discover_connection_sources),
        )
        .route(
            "/api/v1/connections/{id}/registrations",
            get(handlers::registrations::list_registrations),
        )
        .route(
            "/api/v1/connections/{id}/registrations/auto",
            post(handlers::registrations::auto_register_sources),
        )
        .route(
            "/api/v1/connections/{id}/registrations/bulk",
            post(handlers::registrations::bulk_register_sources),
        )
        .route(
            "/api/v1/connections/{id}/virtual-tables/query",
            post(handlers::registrations::query_virtual_table),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::auth_layer,
        ));

    let app = Router::new()
        .merge(public)
        .merge(protected)
        .with_state(state);

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting virtual-table-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
