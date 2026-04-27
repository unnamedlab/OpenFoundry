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
    observability::init_tracing("dataset-versioning-service");

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
        get(|| async { axum::Json(HealthStatus::ok("dataset-versioning-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/datasets/{id}/versions",
            get(handlers::versions::list_versions),
        )
        .route(
            "/api/v1/datasets/{id}/transactions",
            get(handlers::transactions::list_transactions),
        )
        .route(
            "/api/v1/datasets/{id}/snapshots",
            post(handlers::lifecycle::create_snapshot),
        )
        .route(
            "/api/v1/datasets/{id}/transactions/append",
            post(handlers::lifecycle::append_rows),
        )
        .route(
            "/api/v1/datasets/{id}/transactions/update",
            post(handlers::lifecycle::update_rows),
        )
        .route(
            "/api/v1/datasets/{id}/transactions/delete",
            post(handlers::lifecycle::delete_rows),
        )
        .route(
            "/api/v1/datasets/{id}/branches",
            get(handlers::branches::list_branches).post(handlers::branches::create_branch),
        )
        .route(
            "/api/v1/datasets/{id}/branches/{branch_name}/checkout",
            post(handlers::branches::checkout_branch),
        )
        .route(
            "/api/v1/datasets/{id}/branches/{branch_name}/merge",
            post(handlers::branches::merge_branch),
        )
        .route(
            "/api/v1/datasets/{id}/branches/{branch_name}/promote",
            post(handlers::branches::promote_branch),
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
    tracing::info!("starting dataset-versioning-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
