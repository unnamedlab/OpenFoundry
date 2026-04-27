mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    extract::FromRef,
    middleware,
    routing::{get, patch},
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
    observability::init_tracing("retention-policy-service");

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

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("retention-policy-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/retention/policies",
            get(handlers::retention::list_policies).post(handlers::retention::create_policy),
        )
        .route(
            "/api/v1/retention/policies/{id}",
            patch(handlers::retention::update_policy),
        )
        .route(
            "/api/v1/retention/jobs",
            get(handlers::retention::list_jobs).post(handlers::retention::run_job),
        )
        .route(
            "/api/v1/datasets/{id}/retention",
            get(handlers::retention::get_dataset_retention),
        )
        .route(
            "/api/v1/datasets/transactions/{id}/retention",
            get(handlers::retention::get_transaction_retention),
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
    tracing::info!("starting retention-policy-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
