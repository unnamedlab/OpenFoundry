mod config;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{Router, extract::FromRef, middleware, routing::get};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub query_service_url: String,
    pub pipeline_service_url: String,
    pub dataset_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("sql-warehousing-service");

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
        .timeout(std::time::Duration::from_secs(60))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        query_service_url: cfg.query_service_url.clone(),
        pipeline_service_url: cfg.pipeline_service_url.clone(),
        dataset_service_url: cfg.dataset_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("sql-warehousing-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/sql-warehouse/jobs",
            get(handlers::list_jobs).post(handlers::submit_job),
        )
        .route("/api/v1/sql-warehouse/jobs/{id}", get(handlers::get_job))
        .route(
            "/api/v1/sql-warehouse/jobs/{id}/cancel",
            axum::routing::post(handlers::cancel_job),
        )
        .route(
            "/api/v1/sql-warehouse/transformations",
            get(handlers::list_transformations).post(handlers::register_transformation),
        )
        .route(
            "/api/v1/sql-warehouse/transformations/{id}",
            get(handlers::get_transformation),
        )
        .route(
            "/api/v1/sql-warehouse/storage",
            get(handlers::list_storage_artifacts),
        )
        .route(
            "/api/v1/sql-warehouse/storage/{id}",
            get(handlers::get_storage_artifact),
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
    tracing::info!("starting sql-warehousing-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
