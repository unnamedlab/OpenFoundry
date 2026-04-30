//! `connector-management-service` binary entry point.
//!
//! Wires the Axum HTTP server that backs the Data Connection app
//! (`/api/v1/data-connection/*`). The service owns the connector catalog,
//! source CRUD and connection test flow. Egress policy ownership stays in
//! `network-boundary-service`; sync runs stay in `ingestion-replication-service`.

mod config;
mod connectors;
mod credential_crypto;
mod domain;
mod handlers;
mod ingestion_bridge;
mod models;

use std::net::SocketAddr;
use std::time::Duration;

use auth_middleware::jwt::{self, JwtConfig};
use axum::{
    Router,
    extract::{Request, State},
    http::header::AUTHORIZATION,
    middleware::{self, Next},
    response::Response,
    routing::{get, post},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

/// Shared state injected into every Axum handler.
///
/// The fields are the union of what `handlers/connections.rs`,
/// `handlers/catalog.rs` and the per-connector `domain::*` modules read
/// today; do not narrow this struct without auditing every consumer.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub http_client: reqwest::Client,
    pub jwt_config: JwtConfig,
    pub dataset_service_url: String,
    pub pipeline_service_url: String,
    pub ontology_service_url: String,
    pub ingestion_replication_service_url: String,
    /// gRPC endpoint of `ingestion-replication-service`. Empty string disables
    /// the bridge: `run_sync` then records `pending` runs as before.
    pub ingestion_replication_grpc_url: String,
    pub allow_private_network_egress: bool,
    pub allowed_egress_hosts: Vec<String>,
    pub agent_stale_after: Duration,
    /// AES-256-GCM data-encryption key for credential ciphertext at rest.
    pub credential_key: [u8; 32],
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("connector_management_service=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();

    let env_key = std::env::var("CREDENTIAL_ENCRYPTION_KEY").ok();
    if env_key.as_deref().map(str::trim).is_none_or(str::is_empty) {
        tracing::warn!(
            "CREDENTIAL_ENCRYPTION_KEY not set; deriving credential key from JWT_SECRET. \
             SET A DEDICATED 32-byte base64 key in production."
        );
    }
    let credential_key =
        credential_crypto::derive_key(env_key.as_deref(), &app_config.jwt_secret)?;

    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        jwt_config,
        dataset_service_url: app_config.dataset_service_url.clone(),
        pipeline_service_url: app_config.pipeline_service_url.clone(),
        ontology_service_url: app_config.ontology_service_url.clone(),
        ingestion_replication_service_url: app_config
            .ingestion_replication_service_url
            .clone(),
        ingestion_replication_grpc_url: app_config.ingestion_replication_grpc_url.clone(),
        allow_private_network_egress: app_config.allow_private_network_egress,
        allowed_egress_hosts: app_config.allowed_egress_hosts.clone(),
        agent_stale_after: Duration::from_secs(app_config.agent_stale_after_secs),
        credential_key,
    };

    // Data Connection app surface (matches apps/web/src/lib/api/data-connection.ts).
    // Auth is enforced inside the handlers that need claims (e.g. create_connection
    // calls `auth_middleware::layer::AuthUser`); the gallery and reads are open so
    // the frontend renders during bring-up.
    let data_connection = Router::new()
        .route("/catalog", get(handlers::catalog::get_connector_catalog))
        .route(
            "/catalog/contracts",
            get(handlers::catalog::get_connector_contracts),
        )
        .route(
            "/sources",
            get(handlers::connections::list_connections)
                .post(handlers::connections::create_connection),
        )
        .route(
            "/sources/{id}",
            get(handlers::connections::get_connection)
                .delete(handlers::connections::delete_connection),
        )
        .route(
            "/sources/{id}/test-connection",
            post(handlers::connections::test_connection),
        )
        .route(
            "/sources/{id}/capabilities",
            get(handlers::catalog::get_connection_capabilities),
        )
        .route(
            "/sources/{id}/credentials",
            get(handlers::data_connection::list_credentials)
                .post(handlers::data_connection::set_credential),
        )
        .route(
            "/sources/{id}/egress-policies",
            get(handlers::data_connection::list_source_policies)
                .post(handlers::data_connection::attach_policy),
        )
        .route(
            "/sources/{source_id}/egress-policies/{policy_id}",
            axum::routing::delete(handlers::data_connection::detach_policy),
        )
        .route(
            "/sources/{id}/syncs",
            get(handlers::data_connection::list_syncs),
        )
        .route("/syncs", post(handlers::data_connection::create_sync))
        .route("/syncs/{id}/run", post(handlers::data_connection::run_sync))
        .route("/syncs/{id}/runs", get(handlers::data_connection::list_runs))
        // Virtual table registration surface (Foundry "Virtual tables" UI):
        // discovery + bulk register + one-shot auto-register. Recurring
        // auto-registration runs in `domain::auto_registration` and is
        // controlled per-connection via `config.auto_registration.enabled`.
        .route(
            "/sources/{id}/registrations",
            get(handlers::registrations::list_registrations),
        )
        .route(
            "/sources/{id}/registrations/discover",
            post(handlers::registrations::discover),
        )
        .route(
            "/sources/{id}/registrations/bulk",
            post(handlers::registrations::bulk_register),
        )
        .route(
            "/sources/{id}/registrations/auto",
            post(handlers::registrations::auto_register),
        )
        .route(
            "/sources/{source_id}/registrations/{registration_id}",
            axum::routing::delete(handlers::registrations::delete_registration),
        );

    // Backwards-compatible aliases under `/connections` for callers still on
    // the legacy route shape. Same handlers, identical behaviour.
    let connections_alias = Router::new()
        .route(
            "/connections",
            get(handlers::connections::list_connections)
                .post(handlers::connections::create_connection),
        )
        .route(
            "/connections/{id}",
            get(handlers::connections::get_connection)
                .delete(handlers::connections::delete_connection),
        )
        .route(
            "/connections/{id}/test",
            post(handlers::connections::test_connection),
        );

    let app = Router::new()
        .nest(
            "/api/v1",
            {
                let mut v1 = Router::new()
                    .nest("/data-connection", data_connection)
                    .merge(connections_alias);
                // Dev-only auth shim: enabled when OPENFOUNDRY_DEV_AUTH=1.
                // Mounts /api/v1/auth/* and /api/v1/users/me so the SvelteKit
                // app can complete the login flow against this binary while
                // identity-federation-service is not yet wired. See
                // handlers::dev_auth for the contract.
                if std::env::var("OPENFOUNDRY_DEV_AUTH").as_deref() == Ok("1") {
                    tracing::warn!(
                        "OPENFOUNDRY_DEV_AUTH=1 — exposing dev /auth shim. \
                         Do not use this configuration in production."
                    );
                    v1 = v1
                        .route("/auth/login", post(handlers::dev_auth::login))
                        .route("/auth/refresh", post(handlers::dev_auth::refresh))
                        .route(
                            "/auth/bootstrap-status",
                            get(handlers::dev_auth::bootstrap_status),
                        )
                        .route("/users/me", get(handlers::dev_auth::me));
                }
                v1
            },
        )
        .layer(middleware::from_fn_with_state(
            state.jwt_config.clone(),
            optional_auth_layer,
        ))
        .route("/health", get(|| async { "ok" }))
        .with_state(state.clone());

    // Optional background loop that mirrors Foundry's recurring auto-registration.
    // Disabled by default; opt in by exporting
    // `OPENFOUNDRY_AUTO_REGISTRATION_INTERVAL_SECS=<n>` (n > 0). Per-source
    // settings live in `connections.config.auto_registration`.
    if let Ok(raw) = std::env::var("OPENFOUNDRY_AUTO_REGISTRATION_INTERVAL_SECS") {
        if let Ok(secs) = raw.parse::<u64>() {
            if secs > 0 {
                let interval = Duration::from_secs(secs);
                tracing::info!(?interval, "auto-registration scheduler enabled");
                let scheduler_state = state.clone();
                tokio::spawn(async move {
                    domain::auto_registration::run(scheduler_state, interval).await;
                });
            }
        }
    }

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting connector-management-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

/// Best-effort JWT decoder middleware: when the request carries a valid
/// `Authorization: Bearer <token>` header, the resulting `Claims` are inserted
/// into the request extensions so handlers using `auth_middleware::layer::AuthUser`
/// see the caller. Requests without a token (or with an invalid one) fall
/// through unchanged so that public endpoints (catalog, GET sources) keep
/// working during the MVP bring-up.
async fn optional_auth_layer(
    State(config): State<JwtConfig>,
    mut req: Request,
    next: Next,
) -> Response {
    if let Some(token) = req
        .headers()
        .get(AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
    {
        if let Ok(claims) = jwt::decode_token(&config, token) {
            req.extensions_mut().insert(claims);
        }
    }
    next.run(req).await
}
