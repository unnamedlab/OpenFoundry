//! `oauth-integration-service` binary.
//!
//! Stream **S3.4** of the Cassandra/Foundry parity plan: short-lived
//! OAuth/PKCE pending-auth state moves to Cassandra (TTL-driven);
//! long-lived OAuth client + integration config stays in
//! consolidated Postgres (`auth_schema.oauth_clients`).
//!
//! This binary boots tracing, connects Postgres, builds the local
//! [`AppState`] (the substrate-only `lib.rs` purposefully does not
//! export it), and wires the legacy Foundry API surface
//! (applications, oauth_clients, sso, external integrations,
//! api keys) layered with `auth-middleware` JWT auth. Public SSO
//! routes (`/sso/providers`, `/sso/login/*`) are unauthenticated by
//! design.

mod config;
mod domain;
mod handlers;
mod models;

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{delete, get, post},
};
use cassandra_kernel::{ClusterConfig, SessionBuilder};
use config::AppConfig;
use identity_federation_service::sessions_cassandra::SessionsAdapter;
use oauth_integration_service::pending_auth_cassandra::PendingAuthAdapter;
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub sessions: SessionsAdapter,
    pub pending_auth: PendingAuthAdapter,
    pub public_web_origin: String,
    pub saml_service_provider_entity_id: String,
    pub saml_allowed_clock_skew_secs: i64,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("oauth_integration_service=info,tower_http=info")
            }),
        )
        .init();

    let config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&config.database_url)
        .await?;
    let cassandra_session = build_cassandra_session().await?;
    let sessions = SessionsAdapter::new(cassandra_session.clone());
    sessions.migrate().await?;
    let pending_auth = PendingAuthAdapter::new(cassandra_session);
    pending_auth.migrate().await?;

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();
    let state = AppState {
        db,
        jwt_config: jwt_config.clone(),
        sessions,
        pending_auth,
        public_web_origin: config.public_web_origin.clone(),
        saml_service_provider_entity_id: config
            .saml_service_provider_entity_id
            .clone()
            .unwrap_or_default(),
        saml_allowed_clock_skew_secs: config.saml_allowed_clock_skew_secs,
    };

    // Public, unauthenticated SSO surface.
    let public = Router::new()
        .route("/sso/providers", get(handlers::sso::list_public_providers))
        .route(
            "/sso/login/:provider_id/start",
            post(handlers::sso::start_login),
        )
        .route(
            "/sso/login/:provider_id/complete",
            post(handlers::sso::complete_login),
        );

    let admin = Router::new()
        // SSO admin
        .route(
            "/admin/sso/providers",
            get(handlers::sso::list_providers).post(handlers::sso::create_provider),
        )
        .route(
            "/admin/sso/providers/:provider_id",
            post(handlers::sso::update_provider).delete(handlers::sso::delete_provider),
        )
        // Applications
        .route(
            "/admin/applications",
            get(handlers::applications::list_applications)
                .post(handlers::applications::create_application),
        )
        .route(
            "/admin/applications/:application_id",
            post(handlers::applications::update_application),
        )
        .route(
            "/admin/applications/:application_id/credentials",
            get(handlers::applications::list_application_credentials)
                .post(handlers::applications::create_application_credential),
        )
        .route(
            "/admin/applications/:application_id/credentials/:credential_id",
            delete(handlers::applications::revoke_application_credential),
        )
        // OAuth clients
        .route(
            "/admin/oauth-clients",
            get(handlers::oauth_clients::list_oauth_clients)
                .post(handlers::oauth_clients::create_oauth_client),
        )
        .route(
            "/admin/oauth-clients/:client_id",
            post(handlers::oauth_clients::update_oauth_client),
        )
        // API keys
        .route(
            "/admin/api-keys",
            get(handlers::api_key_mgmt::list_api_keys)
                .post(handlers::api_key_mgmt::create_api_key),
        )
        .route(
            "/admin/api-keys/:api_key_id",
            delete(handlers::api_key_mgmt::revoke_api_key),
        )
        // External integrations
        .route(
            "/admin/external-integrations",
            get(handlers::external_integrations::list_external_integrations)
                .post(handlers::external_integrations::create_external_integration),
        )
        .route(
            "/admin/external-integrations/:integration_id",
            post(handlers::external_integrations::update_external_integration),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1", public.merge(admin))
        .route("/health", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting oauth-integration-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn build_cassandra_session()
-> Result<Arc<cassandra_kernel::scylla::Session>, Box<dyn std::error::Error>> {
    let contact_points =
        std::env::var("CASSANDRA_CONTACT_POINTS").unwrap_or_else(|_| "127.0.0.1:9042".to_string());
    let datacenter =
        std::env::var("CASSANDRA_LOCAL_DATACENTER").unwrap_or_else(|_| "dc1".to_string());
    let keyspace = std::env::var("CASSANDRA_KEYSPACE").ok();

    let cluster = ClusterConfig {
        contact_points: contact_points
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect(),
        local_datacenter: datacenter,
        keyspace,
        ..ClusterConfig::dev_local()
    };
    Ok(Arc::new(SessionBuilder::new(cluster).build().await?))
}
