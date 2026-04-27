#![allow(dead_code)]

mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    extract::FromRef,
    middleware,
    routing::{get, post},
};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub audit_service_url: String,
    pub dataset_service_url: String,
    pub ontology_service_url: String,
    pub pipeline_service_url: String,
    pub ai_service_url: String,
    pub search_embedding_provider: String,
    pub notification_service_url: String,
    pub node_runtime_command: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("ontology-funnel-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()
        .expect("failed to build ontology HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        audit_service_url: cfg.audit_service_url.clone(),
        dataset_service_url: cfg.dataset_service_url.clone(),
        ontology_service_url: cfg.ontology_service_url.clone(),
        pipeline_service_url: cfg.pipeline_service_url.clone(),
        ai_service_url: cfg.ai_service_url.clone(),
        search_embedding_provider: cfg.search_embedding_provider.clone(),
        notification_service_url: cfg.notification_service_url.clone(),
        node_runtime_command: cfg.node_runtime_command.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("ontology-funnel-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/funnel/health",
            get(handlers::funnel::get_funnel_health),
        )
        .route(
            "/api/v1/ontology/storage/insights",
            get(handlers::storage::get_storage_insights),
        )
        .route(
            "/api/v1/ontology/funnel/sources",
            get(handlers::funnel::list_funnel_sources).post(handlers::funnel::create_funnel_source),
        )
        .route(
            "/api/v1/ontology/funnel/sources/{id}",
            get(handlers::funnel::get_funnel_source)
                .patch(handlers::funnel::update_funnel_source)
                .delete(handlers::funnel::delete_funnel_source),
        )
        .route(
            "/api/v1/ontology/funnel/sources/{id}/health",
            get(handlers::funnel::get_funnel_source_health),
        )
        .route(
            "/api/v1/ontology/funnel/sources/{id}/run",
            post(handlers::funnel::trigger_funnel_run),
        )
        .route(
            "/api/v1/ontology/funnel/sources/{id}/runs",
            get(handlers::funnel::list_funnel_runs),
        )
        .route(
            "/api/v1/ontology/funnel/sources/{source_id}/runs/{run_id}",
            get(handlers::funnel::get_funnel_run),
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
    tracing::info!("starting ontology-funnel-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
