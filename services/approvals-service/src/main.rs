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
    pub workflow_service_url: String,
    pub ontology_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("approvals-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .expect("failed to build approvals HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        workflow_service_url: cfg.workflow_service_url.clone(),
        ontology_service_url: cfg.ontology_service_url.clone(),
    };

    let public = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("approvals-service")) }),
        )
        .route(
            "/internal/approvals",
            post(handlers::approvals::create_approval),
        );

    let protected = Router::new()
        .route(
            "/api/v1/approvals",
            get(handlers::approvals::list_approvals),
        )
        .route(
            "/api/v1/approvals/{id}/decision",
            post(handlers::approvals::decide_approval),
        )
        .route(
            "/api/v1/workflows/approvals",
            get(handlers::approvals::list_approvals),
        )
        .route(
            "/api/v1/workflows/approvals/{id}/decision",
            post(handlers::approvals::decide_approval),
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
    tracing::info!("starting approvals-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
