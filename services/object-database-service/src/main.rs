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
    routing::{delete, get, patch, post},
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
    observability::init_tracing("object-database-service");

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
        get(|| async { axum::Json(HealthStatus::ok("object-database-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/types/{type_id}/objects",
            post(handlers::objects::create_object),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects",
            get(handlers::objects::list_objects),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}",
            get(handlers::objects::get_object),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}",
            patch(handlers::objects::update_object),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}",
            delete(handlers::objects::delete_object),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}/neighbors",
            get(handlers::objects::list_neighbors),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}/view",
            get(handlers::objects::get_object_view),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}/simulate",
            post(handlers::objects::simulate_object),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/{obj_id}/scenarios/simulate",
            post(handlers::objects::simulate_object_scenarios),
        )
        .route(
            "/api/v1/ontology/links/{link_type_id}/instances",
            post(handlers::links::create_link),
        )
        .route(
            "/api/v1/ontology/links/{link_type_id}/instances",
            get(handlers::links::list_links),
        )
        .route(
            "/api/v1/ontology/links/{link_type_id}/instances/{link_id}",
            delete(handlers::links::delete_link),
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
    tracing::info!("starting object-database-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
