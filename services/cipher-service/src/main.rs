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
    observability::init_tracing("cipher-service");

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
        get(|| async { axum::Json(HealthStatus::ok("cipher-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/auth/cipher/hash",
            post(handlers::cipher::hash_content),
        )
        .route(
            "/api/v1/auth/cipher/sign",
            post(handlers::cipher::sign_content),
        )
        .route(
            "/api/v1/auth/cipher/verify",
            post(handlers::cipher::verify_signature),
        )
        .route(
            "/api/v1/auth/cipher/encrypt",
            post(handlers::cipher::encrypt_content),
        )
        .route(
            "/api/v1/auth/cipher/decrypt",
            post(handlers::cipher::decrypt_content),
        )
        .route(
            "/api/v1/auth/cipher/permissions",
            get(handlers::cipher::list_permissions),
        )
        .route(
            "/api/v1/auth/cipher/channels",
            get(handlers::cipher::list_channels).post(handlers::cipher::create_channel),
        )
        .route(
            "/api/v1/auth/cipher/licenses",
            get(handlers::cipher::list_licenses).post(handlers::cipher::create_license),
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
    tracing::info!("starting cipher-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
