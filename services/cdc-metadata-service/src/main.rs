mod config;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    extract::FromRef,
    middleware,
    routing::get,
};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub data_connector_url: String,
    pub event_streaming_service_url: String,
    pub pipeline_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("cdc-metadata-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        data_connector_url: cfg.data_connector_url.clone(),
        event_streaming_service_url: cfg.event_streaming_service_url.clone(),
        pipeline_service_url: cfg.pipeline_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("cdc-metadata-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/cdc-metadata/streams",
            get(handlers::list_streams).post(handlers::register_stream),
        )
        .route(
            "/api/v1/cdc-metadata/streams/{id}",
            get(handlers::get_stream),
        )
        .route(
            "/api/v1/cdc-metadata/streams/{id}/checkpoints",
            get(handlers::get_checkpoint).post(handlers::record_checkpoint),
        )
        .route(
            "/api/v1/cdc-metadata/streams/{id}/resolution",
            get(handlers::get_resolution).post(handlers::update_resolution),
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
    tracing::info!("starting cdc-metadata-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
