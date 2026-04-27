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
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("ml-experiments-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("ml-experiments-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ml/experiments",
            get(handlers::experiments::list_experiments)
                .post(handlers::experiments::create_experiment),
        )
        .route(
            "/api/v1/ml/experiments/{id}",
            axum::routing::patch(handlers::experiments::update_experiment),
        )
        .route(
            "/api/v1/ml/experiments/{id}/asset-lineage",
            get(handlers::experiments::get_experiment_asset_lineage),
        )
        .route(
            "/api/v1/ml/experiments/{id}/runs",
            get(handlers::experiments::list_runs).post(handlers::experiments::create_run),
        )
        .route(
            "/api/v1/ml/runs/{id}",
            axum::routing::patch(handlers::experiments::update_run),
        )
        .route(
            "/api/v1/ml/runs/compare",
            post(handlers::experiments::compare_runs),
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
    tracing::info!("starting ml-experiments-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
