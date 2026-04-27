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
    observability::init_tracing("ontology-query-service");

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
        get(|| async { axum::Json(HealthStatus::ok("ontology-query-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/types/{type_id}/objects/query",
            post(handlers::objects::query_objects),
        )
        .route(
            "/api/v1/ontology/types/{type_id}/objects/knn",
            post(handlers::objects::knn_objects),
        )
        .route(
            "/api/v1/ontology/search",
            post(handlers::search::search_ontology),
        )
        .route("/api/v1/ontology/graph", get(handlers::search::get_graph))
        .route(
            "/api/v1/ontology/quiver/vega-spec",
            post(handlers::search::get_quiver_vega_spec),
        )
        .route(
            "/api/v1/ontology/quiver/visual-functions",
            get(handlers::search::list_quiver_visual_functions)
                .post(handlers::search::create_quiver_visual_function),
        )
        .route(
            "/api/v1/ontology/quiver/visual-functions/{id}",
            get(handlers::search::get_quiver_visual_function)
                .patch(handlers::search::update_quiver_visual_function)
                .delete(handlers::search::delete_quiver_visual_function),
        )
        .route(
            "/api/v1/ontology/object-sets",
            get(handlers::object_sets::list_object_sets)
                .post(handlers::object_sets::create_object_set),
        )
        .route(
            "/api/v1/ontology/object-sets/{id}",
            get(handlers::object_sets::get_object_set)
                .patch(handlers::object_sets::update_object_set)
                .delete(handlers::object_sets::delete_object_set),
        )
        .route(
            "/api/v1/ontology/object-sets/{id}/evaluate",
            post(handlers::object_sets::evaluate_object_set),
        )
        .route(
            "/api/v1/ontology/object-sets/{id}/materialize",
            post(handlers::object_sets::materialize_object_set),
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
    tracing::info!("starting ontology-query-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
