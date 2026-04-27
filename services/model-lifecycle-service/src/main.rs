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
    pub model_catalog_service_url: String,
    pub model_deployment_service_url: String,
    pub approvals_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self { state.jwt_config.clone() }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("model-lifecycle-service");
    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");
    sqlx::migrate!("./migrations").run(&pool).await.expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        model_catalog_service_url: cfg.model_catalog_service_url.clone(),
        model_deployment_service_url: cfg.model_deployment_service_url.clone(),
        approvals_service_url: cfg.approvals_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("model-lifecycle-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ml/lifecycle/submissions",
            get(handlers::list_submissions).post(handlers::create_submission),
        )
        .route(
            "/api/v1/ml/lifecycle/submissions/{id}",
            get(handlers::get_submission),
        )
        .route(
            "/api/v1/ml/lifecycle/submissions/{id}/transition",
            axum::routing::post(handlers::transition_submission),
        )
        .route(
            "/api/v1/ml/lifecycle/objectives",
            get(handlers::list_objectives).post(handlers::create_objective),
        )
        .layer(middleware::from_fn_with_state(jwt_config, auth_middleware::auth_layer));

    let app = Router::new().merge(public).merge(protected).with_state(state);
    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting model-lifecycle-service on {addr}");
    let listener = tokio::net::TcpListener::bind(&addr).await.expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
