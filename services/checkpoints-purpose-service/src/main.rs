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
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("checkpoints-purpose-service");

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
    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
    };

    let public = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("checkpoints-purpose-service")) }),
        )
        .route(
            "/internal/checkpoints-purpose/evaluate",
            post(handlers::checkpoints::evaluate_checkpoint_internal),
        );

    let protected = Router::new()
        .route(
            "/api/v1/checkpoints-purpose/policies",
            get(handlers::checkpoints::list_policies).post(handlers::checkpoints::create_policy),
        )
        .route(
            "/api/v1/checkpoints-purpose/templates",
            get(handlers::checkpoints::list_templates),
        )
        .route(
            "/api/v1/checkpoints-purpose/sensitive-interactions",
            get(handlers::checkpoints::list_sensitive_configs),
        )
        .route(
            "/api/v1/checkpoints-purpose/checkpoints/evaluate",
            post(handlers::checkpoints::evaluate_checkpoint),
        )
        .route(
            "/api/v1/checkpoints-purpose/records",
            get(handlers::checkpoints::list_records),
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
    tracing::info!("starting checkpoints-purpose-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
