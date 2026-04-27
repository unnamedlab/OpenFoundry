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
    routing::{delete, get, post, put},
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
    observability::init_tracing("ontology-actions-service");

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
        get(|| async { axum::Json(HealthStatus::ok("ontology-actions-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/actions",
            post(handlers::actions::create_action_type),
        )
        .route(
            "/api/v1/ontology/actions",
            get(handlers::actions::list_action_types),
        )
        .route(
            "/api/v1/ontology/actions/{id}",
            get(handlers::actions::get_action_type),
        )
        .route(
            "/api/v1/ontology/actions/{id}",
            put(handlers::actions::update_action_type),
        )
        .route(
            "/api/v1/ontology/actions/{id}",
            delete(handlers::actions::delete_action_type),
        )
        .route(
            "/api/v1/ontology/actions/{id}/validate",
            post(handlers::actions::validate_action),
        )
        .route(
            "/api/v1/ontology/actions/{id}/execute",
            post(handlers::actions::execute_action),
        )
        .route(
            "/api/v1/ontology/actions/{id}/what-if",
            get(handlers::actions::list_action_what_if_branches)
                .post(handlers::actions::create_action_what_if_branch),
        )
        .route(
            "/api/v1/ontology/actions/{id}/what-if/{branch_id}",
            delete(handlers::actions::delete_action_what_if_branch),
        )
        .route(
            "/api/v1/ontology/actions/{id}/execute-batch",
            post(handlers::actions::execute_action_batch),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}/inline-edit/{property_id}",
            post(handlers::actions::execute_inline_edit),
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
    tracing::info!("starting ontology-actions-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
