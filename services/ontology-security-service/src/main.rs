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
    observability::init_tracing("ontology-security-service");

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
        get(|| async { axum::Json(HealthStatus::ok("ontology-security-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/projects",
            get(handlers::projects::list_projects).post(handlers::projects::create_project),
        )
        .route(
            "/api/v1/ontology/projects/{id}",
            get(handlers::projects::get_project)
                .patch(handlers::projects::update_project)
                .delete(handlers::projects::delete_project),
        )
        .route(
            "/api/v1/ontology/projects/{id}/memberships",
            get(handlers::projects::list_project_memberships)
                .post(handlers::projects::upsert_project_membership),
        )
        .route(
            "/api/v1/ontology/projects/{id}/memberships/{user_id}",
            delete(handlers::projects::delete_project_membership),
        )
        .route(
            "/api/v1/ontology/projects/{id}/resources",
            get(handlers::projects::list_project_resources)
                .post(handlers::projects::bind_project_resource),
        )
        .route(
            "/api/v1/ontology/projects/{id}/resources/{resource_kind}/{resource_id}",
            delete(handlers::projects::unbind_project_resource),
        )
        .route(
            "/api/v1/ontology/rules",
            get(handlers::rules::list_rules).post(handlers::rules::create_rule),
        )
        .route(
            "/api/v1/ontology/rules/insights",
            get(handlers::rules::get_machinery_insights),
        )
        .route(
            "/api/v1/ontology/rules/machinery/queue",
            get(handlers::rules::get_machinery_queue),
        )
        .route(
            "/api/v1/ontology/rules/machinery/queue/{id}",
            patch(handlers::rules::update_machinery_queue_item),
        )
        .route(
            "/api/v1/ontology/rules/{id}",
            get(handlers::rules::get_rule)
                .patch(handlers::rules::update_rule)
                .delete(handlers::rules::delete_rule),
        )
        .route(
            "/api/v1/ontology/rules/{id}/simulate",
            post(handlers::rules::simulate_rule),
        )
        .route(
            "/api/v1/ontology/rules/{id}/apply",
            post(handlers::rules::apply_rule),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/rules",
            get(handlers::rules::list_rules_for_object_type),
        )
        .route(
            "/api/v1/ontology/objects/{obj_id}/rule-runs",
            get(handlers::rules::list_object_rule_runs),
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
    tracing::info!("starting ontology-security-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
