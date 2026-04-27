mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{delete, get, post, put},
};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

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
    observability::init_tracing("oauth-integration-service");

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
    let saml_service_provider_entity_id = cfg.saml_service_provider_entity_id.unwrap_or_else(|| {
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

    let public = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("oauth-integration-service")) }),
        )
        .route(
            "/api/v1/auth/sso/providers/public",
            get(handlers::sso::list_public_providers),
        )
        .route(
            "/api/v1/auth/sso/providers/{slug}/start",
            get(handlers::sso::start_login),
        )
        .route(
            "/api/v1/auth/sso/callback",
            post(handlers::sso::complete_login),
        );

    let protected = Router::new()
        .route(
            "/api/v1/api-keys",
            get(handlers::api_key_mgmt::list_api_keys).post(handlers::api_key_mgmt::create_api_key),
        )
        .route(
            "/api/v1/api-keys/{id}",
            delete(handlers::api_key_mgmt::revoke_api_key),
        )
        .route(
            "/api/v1/auth/sso/providers",
            get(handlers::sso::list_providers).post(handlers::sso::create_provider),
        )
        .route(
            "/api/v1/auth/sso/providers/{id}",
            put(handlers::sso::update_provider).delete(handlers::sso::delete_provider),
        )
        .route(
            "/api/v1/applications",
            get(handlers::applications::list_applications)
                .post(handlers::applications::create_application),
        )
        .route(
            "/api/v1/applications/{id}",
            put(handlers::applications::update_application),
        )
        .route(
            "/api/v1/applications/{application_id}/credentials",
            get(handlers::applications::list_application_credentials)
                .post(handlers::applications::create_application_credential),
        )
        .route(
            "/api/v1/applications/{application_id}/credentials/{credential_id}",
            delete(handlers::applications::revoke_application_credential),
        )
        .route(
            "/api/v1/oauth/clients",
            get(handlers::oauth_clients::list_oauth_clients)
                .post(handlers::oauth_clients::create_oauth_client),
        )
        .route(
            "/api/v1/oauth/clients/{id}",
            put(handlers::oauth_clients::update_oauth_client),
        )
        .route(
            "/api/v1/external-integrations",
            get(handlers::external_integrations::list_external_integrations)
                .post(handlers::external_integrations::create_external_integration),
        )
        .route(
            "/api/v1/external-integrations/{id}",
            put(handlers::external_integrations::update_external_integration),
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
    tracing::info!("starting oauth-integration-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
