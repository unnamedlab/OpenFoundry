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
    observability::init_tracing("network-boundary-service");

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
            get(|| async { axum::Json(HealthStatus::ok("network-boundary-service")) }),
        )
        .route(
            "/internal/network-boundaries/egress/validate",
            post(handlers::boundary::validate_egress),
        );

    let protected = Router::new()
        .route(
            "/api/v1/network-boundaries/policies",
            get(handlers::boundary::list_policies).post(handlers::boundary::create_policy),
        )
        .route(
            "/api/v1/network-boundaries/ingress-policies",
            get(handlers::boundary::list_ingress_policies).post(handlers::boundary::create_policy),
        )
        .route(
            "/api/v1/network-boundaries/egress-policies",
            get(handlers::boundary::list_egress_policies).post(handlers::boundary::create_policy),
        )
        .route(
            "/api/v1/network-boundaries/private-links",
            get(handlers::boundary::list_private_links)
                .post(handlers::boundary::create_private_link),
        )
        .route(
            "/api/v1/network-boundaries/proxies",
            get(handlers::boundary::list_proxies).post(handlers::boundary::create_proxy),
        )
        .route(
            "/api/v1/network-boundaries/egress/validate",
            post(handlers::boundary::validate_egress),
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
    tracing::info!("starting network-boundary-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
