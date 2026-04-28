mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{delete, get, patch, post},
};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

/// Shared application state passed to all handlers.
#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    let cfg = config::AppConfig::from_env().expect("failed to load config");
    if let Some(nats_url) = cfg.nats_url.as_deref() {
        tracing::info!(
            nats_url,
            "identity-federation-service NATS integration configured"
        );
    }
    if let Some(redis_url) = cfg.redis_url.as_deref() {
        tracing::info!(
            redis_url,
            "identity-federation-service Redis integration configured"
        );
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

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
    };

    // Public routes (no auth required)
    let public = Router::new()
        .route("/health", get(|| async { "ok" }))
        .route("/api/v1/auth/register", post(handlers::register::register))
        .route("/api/v1/auth/login", post(handlers::login::login))
        .route("/api/v1/auth/refresh", post(handlers::token::refresh))
        .route(
            "/api/v1/auth/mfa/complete",
            post(handlers::mfa::complete_login),
        );

    // Protected routes (auth required)
    let protected = Router::new()
        .route("/api/v1/users/me", get(handlers::user_mgmt::me))
        .route("/api/v2/admin/users/me", get(handlers::user_mgmt::me))
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
        .route(
            "/api/v1/auth/sessions",
            get(handlers::sessions::list_scoped_sessions),
        )
        .route(
            "/api/v1/auth/sessions/scoped",
            post(handlers::sessions::create_scoped_session),
        )
        .route(
            "/api/v1/auth/sessions/guest",
            post(handlers::sessions::create_guest_session),
        )
        .route(
            "/api/v1/auth/sessions/{id}",
            delete(handlers::sessions::revoke_scoped_session),
        )
        .route(
            "/api/v1/auth/mfa",
            get(handlers::mfa::status).delete(handlers::mfa::disable),
        )
        .route("/api/v1/auth/mfa/enroll", post(handlers::mfa::enroll))
        .route("/api/v1/auth/mfa/verify", post(handlers::mfa::verify_setup))
        .layer(middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::auth_layer,
        ));

    let app = Router::new()
        .merge(public)
        .merge(protected)
        .with_state(state);

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting identity-federation-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
