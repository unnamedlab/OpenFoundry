mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{get, patch},
};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

/// Shared application state passed to all handlers.
#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub public_web_origin: String,
    pub saml_service_provider_entity_id: String,
    pub saml_allowed_clock_skew_secs: i64,
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    let cfg = config::AppConfig::from_env().expect("failed to load config");
    if let Some(nats_url) = cfg.nats_url.as_deref() {
        tracing::info!(nats_url, "auth-service NATS integration configured");
    }
    if let Some(redis_url) = cfg.redis_url.as_deref() {
        tracing::info!(redis_url, "auth-service Redis integration configured");
    }

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    // Run migrations
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret)
        .with_access_ttl(cfg.jwt_access_ttl_secs)
        .with_refresh_ttl(cfg.jwt_refresh_ttl_secs)
        .with_env_defaults();
    let saml_service_provider_entity_id =
        cfg.saml_service_provider_entity_id.unwrap_or_else(|| {
            format!(
                "{}/auth/saml/metadata",
                cfg.public_web_origin.trim_end_matches('/')
            )
        });

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        public_web_origin: cfg.public_web_origin.clone(),
        saml_service_provider_entity_id,
        saml_allowed_clock_skew_secs: cfg.saml_allowed_clock_skew_secs,
    };

    // Public routes (no auth required)
    let public = Router::new().route("/health", get(|| async { "ok" }));

    // Protected routes (auth required)
    let protected = Router::new()
        .route("/api/v1/users", get(handlers::user_mgmt::list_users))
        .route("/api/v2/admin/users", get(handlers::user_mgmt::list_users))
        .route(
            "/api/v1/users/{id}",
            patch(handlers::user_mgmt::update_user).delete(handlers::user_mgmt::deactivate_user),
        )
        .route(
            "/api/v2/admin/users/{id}",
            patch(handlers::user_mgmt::update_user).delete(handlers::user_mgmt::deactivate_user),
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
    tracing::info!("starting auth-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
