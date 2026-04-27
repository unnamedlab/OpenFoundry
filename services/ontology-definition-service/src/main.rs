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
    routing::{delete, get, patch, post, put},
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
    observability::init_tracing("ontology-definition-service");

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
        get(|| async { axum::Json(HealthStatus::ok("ontology-definition-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/types",
            post(handlers::types::create_object_type),
        )
        .route(
            "/api/v1/ontology/types",
            get(handlers::types::list_object_types),
        )
        .route(
            "/api/v1/ontology/types/{id}",
            get(handlers::types::get_object_type),
        )
        .route(
            "/api/v1/ontology/types/{id}",
            put(handlers::types::update_object_type),
        )
        .route(
            "/api/v1/ontology/types/{id}",
            delete(handlers::types::delete_object_type),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/properties",
            get(handlers::properties::list_properties).post(handlers::properties::create_property),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/properties/{property_id}",
            patch(handlers::properties::update_property)
                .delete(handlers::properties::delete_property),
        )
        .route(
            "/api/v1/ontology/interfaces",
            get(handlers::interfaces::list_interfaces).post(handlers::interfaces::create_interface),
        )
        .route(
            "/api/v1/ontology/interfaces/{id}",
            get(handlers::interfaces::get_interface)
                .patch(handlers::interfaces::update_interface)
                .delete(handlers::interfaces::delete_interface),
        )
        .route(
            "/api/v1/ontology/interfaces/{id}/properties",
            get(handlers::interfaces::list_interface_properties)
                .post(handlers::interfaces::create_interface_property),
        )
        .route(
            "/api/v1/ontology/interfaces/{id}/properties/{property_id}",
            patch(handlers::interfaces::update_interface_property)
                .delete(handlers::interfaces::delete_interface_property),
        )
        .route(
            "/api/v1/ontology/shared-property-types",
            get(handlers::shared_properties::list_shared_property_types)
                .post(handlers::shared_properties::create_shared_property_type),
        )
        .route(
            "/api/v1/ontology/shared-property-types/{id}",
            get(handlers::shared_properties::get_shared_property_type)
                .patch(handlers::shared_properties::update_shared_property_type)
                .delete(handlers::shared_properties::delete_shared_property_type),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/interfaces",
            get(handlers::interfaces::list_type_interfaces),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/interfaces/{interface_id}",
            post(handlers::interfaces::attach_interface_to_type)
                .delete(handlers::interfaces::detach_interface_from_type),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/shared-property-types",
            get(handlers::shared_properties::list_type_shared_property_types),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/shared-property-types/{shared_property_type_id}",
            post(handlers::shared_properties::attach_shared_property_type_to_type)
                .delete(handlers::shared_properties::detach_shared_property_type_from_type),
        )
        .route(
            "/api/v1/ontology/links",
            post(handlers::links::create_link_type),
        )
        .route(
            "/api/v1/ontology/links",
            get(handlers::links::list_link_types),
        )
        .route(
            "/api/v1/ontology/links/{id}",
            patch(handlers::links::update_link_type).delete(handlers::links::delete_link_type),
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
    tracing::info!("starting ontology-definition-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
