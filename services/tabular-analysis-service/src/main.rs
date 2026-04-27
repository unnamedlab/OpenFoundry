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
    pub dataset_service_url: String,
    pub query_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self { state.jwt_config.clone() }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("tabular-analysis-service");
    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let pool = PgPoolOptions::new()
        .max_connections(15)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");
    sqlx::migrate!("./migrations").run(&pool).await.expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(60))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        dataset_service_url: cfg.dataset_service_url.clone(),
        query_service_url: cfg.query_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("tabular-analysis-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/tabular-analysis/jobs",
            get(handlers::list_jobs).post(handlers::submit_job),
        )
        .route(
            "/api/v1/tabular-analysis/jobs/{id}",
            get(handlers::get_job),
        )
        .route(
            "/api/v1/tabular-analysis/jobs/{id}/results",
            get(handlers::list_results).post(handlers::publish_result),
        )
        .layer(middleware::from_fn_with_state(jwt_config, auth_middleware::auth_layer));

    let app = Router::new().merge(public).merge(protected).with_state(state);
    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting tabular-analysis-service on {addr}");
    let listener = tokio::net::TcpListener::bind(&addr).await.expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
