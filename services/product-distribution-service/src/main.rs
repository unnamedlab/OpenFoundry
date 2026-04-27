#![allow(dead_code)]

mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{Router, extract::FromRef, middleware, routing::get};
use sqlx::postgres::PgPoolOptions;
use std::time::Duration;
use tracing_subscriber::EnvFilter;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub app_builder_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .build()
        .expect("failed to build product distribution HTTP client");
    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        app_builder_service_url: cfg.app_builder_service_url.clone(),
    };
    let reconciler_state = state.clone();
    let reconciler_interval = cfg.devops_reconciler_interval_seconds.max(30);

    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(Duration::from_secs(reconciler_interval));
        ticker.tick().await;
        loop {
            if let Err(error) =
                handlers::devops::reconcile_auto_upgrade_fleets(reconciler_state.clone()).await
            {
                tracing::warn!("product distribution reconciler failed: {error}");
            }
            ticker.tick().await;
        }
    });

    let public = Router::new().route("/health", get(|| async { "ok" }));

    let protected = Router::new()
        .route(
            "/api/v1/marketplace/devops/fleets",
            get(handlers::devops::list_fleets).post(handlers::devops::create_fleet),
        )
        .route(
            "/api/v1/marketplace/devops/fleets/{id}/sync",
            axum::routing::post(handlers::devops::sync_fleet),
        )
        .route(
            "/api/v1/marketplace/devops/fleets/{id}/promotion-gates",
            get(handlers::devops::list_promotion_gates)
                .post(handlers::devops::create_promotion_gate),
        )
        .route(
            "/api/v1/marketplace/devops/promotion-gates/{id}",
            axum::routing::patch(handlers::devops::update_promotion_gate),
        )
        .route(
            "/api/v1/marketplace/devops/branches",
            get(handlers::devops::list_enrollment_branches)
                .post(handlers::devops::create_enrollment_branch),
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
    tracing::info!("starting product-distribution-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
