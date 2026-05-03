//! `identity-federation-service` binary.
//!
//! Stream **S3.1 / S3.2** of the Cassandra/Foundry parity plan.
//! Sessions and refresh tokens move to Cassandra (`auth_runtime.*`);
//! Postgres remains for control-plane reads (users/groups/policies).
//!
//! The full handler surface migrates PR-by-PR; this binary boots
//! tracing, connects Postgres + Cassandra, builds the
//! [`AppState`](identity_federation_service::AppState) and exposes
//! the cutover-ready endpoints (`/login`, `/register`, `/token/refresh`,
//! `/users/bootstrap`, `/health`). Remaining routes (mfa, control-panel,
//! group/role/permission/policy CRUD) get layered in by follow-up PRs
//! per the substrate-first cadence in ADR-0024.

mod config;

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, post},
};
use cassandra_kernel::{ClusterConfig, SessionBuilder};
use config::AppConfig;
use event_bus_data::{DataBusConfig, KafkaPublisher, ServicePrincipal};
use identity_federation_service::{
    AppState, cedar_authz, handlers,
    hardening::{
        audit_topic::{
            AuditFailurePolicy, AuditMetrics, IdentityAuditService, KafkaIdentityAuditPublisher,
        },
        jwks_rotation::{JwksRotationService, PostgresJwksKeyStore},
        rate_limit::{InMemoryRateLimiter, RateLimiter, RedisRateLimiter},
        vault_signer::{RotationPolicy, VaultTransitSigner},
        webauthn::{CassandraWebAuthnStore, RelyingPartyConfig, WebAuthnService},
    },
    sessions_cassandra::SessionsAdapter,
};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("identity_federation_service=info,tower_http=info")
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

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();
    let jwks = build_jwks_service(db.clone()).await?;
    let webauthn = build_webauthn_service(cassandra_session).await?;
    let audit = build_audit_service()?;
    let rate_limiter = build_rate_limiter(config.redis_url.as_deref()).await?;

    // S3.1.i — bootstrap the Cedar engine from the bundled
    // `policies/identity_admin.cedar` set so admin endpoints fall under
    // policy enforcement from the first request. Hot-reload via the
    // `authz.policy.changed` NATS subject is wired below; failures are
    // logged but do not block boot (we keep the bundle in memory).
    let authz_engine = cedar_authz::bootstrap_engine().await?;
    if let Ok(nats_url) = std::env::var("NATS_URL") {
        let trimmed = nats_url.trim().to_string();
        if !trimmed.is_empty() {
            let engine = Arc::clone(&authz_engine);
            match cedar_authz::spawn_policy_reload(&trimmed, engine).await {
                Ok(()) => tracing::info!(
                    nats_url = %trimmed,
                    "subscribed to authz.policy.changed for cedar hot-reload"
                ),
                Err(error) => tracing::warn!(
                    %error,
                    "cedar policy reload subscriber unavailable; serving boot-time bundle only"
                ),
            }
        }
    }

    let state = AppState {
        db,
        jwt_config: jwt_config.clone(),
        sessions,
        jwks,
        webauthn,
        audit,
        rate_limiter,
        authz: Arc::clone(&authz_engine),
    };

    let public = Router::new()
        .route("/auth/login", post(handlers::login::login))
        .route("/auth/register", post(handlers::register::register))
        .route(
            "/auth/bootstrap-status",
            get(handlers::register::bootstrap_status),
        )
        .route("/auth/token/refresh", post(handlers::token::refresh))
        .route(
            "/auth/mfa/webauthn/login/challenge",
            post(handlers::mfa::webauthn_login_challenge),
        )
        .route(
            "/auth/mfa/webauthn/login/finish",
            post(handlers::mfa::webauthn_login_finish),
        );

    let protected = Router::new()
        .route(
            "/auth/mfa/webauthn/register/challenge",
            post(handlers::mfa::webauthn_register_challenge),
        )
        .route(
            "/auth/mfa/webauthn/register/finish",
            post(handlers::mfa::webauthn_register_finish),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let scim_engine = Arc::clone(&authz_engine);
    let admin_engine = Arc::clone(&authz_engine);

    let scim = Router::new()
        .route(
            "/scim/v2/ServiceProviderConfig",
            get(handlers::scim::service_provider_config_handler),
        )
        .route("/scim/v2/Schemas", get(handlers::scim::list_schemas))
        .route("/scim/v2/Schemas/{id}", get(handlers::scim::get_schema))
        .route(
            "/scim/v2/ResourceTypes",
            get(handlers::scim::list_resource_types),
        )
        .route(
            "/scim/v2/ResourceTypes/{id}",
            get(handlers::scim::get_resource_type),
        )
        .route(
            "/scim/v2/Users",
            get(handlers::scim::list_users).post(handlers::scim::create_user),
        )
        .route(
            "/scim/v2/Users/{id}",
            get(handlers::scim::get_user)
                .patch(handlers::scim::patch_user)
                .delete(handlers::scim::delete_user),
        )
        .route(
            "/scim/v2/Groups",
            get(handlers::scim::list_groups).post(handlers::scim::create_group),
        )
        .route(
            "/scim/v2/Groups/{id}",
            get(handlers::scim::get_group)
                .patch(handlers::scim::patch_group)
                .delete(handlers::scim::delete_group),
        )
        .layer(axum::Extension(scim_engine))
        .layer(axum::middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let admin = Router::new()
        .route("/jwks/rotate", post(handlers::security_ops::rotate_jwks))
        .route(
            "/jwks/rollback",
            post(handlers::security_ops::rollback_jwks),
        )
        .route("/audit/metrics", get(handlers::security_ops::audit_metrics))
        .layer(axum::Extension(admin_engine))
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1", public)
        .nest("/api/v1", protected)
        .merge(scim)
        .nest("/_admin", admin)
        .route(
            "/.well-known/jwks.json",
            get(handlers::security_ops::publish_jwks),
        )
        .route("/health", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting identity-federation-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn build_webauthn_service(
    session: Arc<cassandra_kernel::scylla::Session>,
) -> Result<WebAuthnService, Box<dyn std::error::Error>> {
    let service = WebAuthnService::new(
        RelyingPartyConfig::from_env(),
        Arc::new(CassandraWebAuthnStore::new(session)),
    );
    service.ensure_schema().await?;
    Ok(service)
}

async fn build_jwks_service(
    db: sqlx::PgPool,
) -> Result<Option<JwksRotationService>, Box<dyn std::error::Error>> {
    if std::env::var("VAULT_ADDR")
        .ok()
        .map(|value| value.trim().is_empty())
        .unwrap_or(true)
    {
        tracing::warn!("VAULT_ADDR is not set; JWKS Vault rotation endpoints disabled");
        return Ok(None);
    }

    let signer = Arc::new(VaultTransitSigner::from_env()?);
    let service = JwksRotationService::new(
        Arc::new(PostgresJwksKeyStore::new(db)),
        signer,
        RotationPolicy::ASVS_L2_DEFAULT,
    );
    service.ensure_schema().await?;
    Ok(Some(service))
}

fn build_audit_service() -> Result<IdentityAuditService, Box<dyn std::error::Error>> {
    let enabled = std::env::var("IDENTITY_AUDIT_ENABLED")
        .ok()
        .map(|value| matches!(value.to_ascii_lowercase().as_str(), "1" | "true" | "yes"))
        .unwrap_or_else(|| std::env::var("KAFKA_BOOTSTRAP_SERVERS").is_ok());
    if !enabled {
        tracing::warn!(
            "identity audit publisher disabled; audit.identity.v1 will not receive events"
        );
        return Ok(IdentityAuditService::disabled());
    }

    let brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS")?;
    let service_name = std::env::var("KAFKA_SERVICE_NAME")
        .unwrap_or_else(|_| "identity-federation-service".to_string());
    let principal = match std::env::var("KAFKA_SASL_PASSWORD") {
        Ok(password) => ServicePrincipal::scram_sha_512(service_name, password),
        Err(_) => ServicePrincipal::insecure_dev(service_name),
    };
    let config = DataBusConfig::new(brokers, principal);
    let publisher = KafkaPublisher::new(&config)?;
    Ok(IdentityAuditService::new(
        Arc::new(KafkaIdentityAuditPublisher::new(Arc::new(publisher))),
        AuditFailurePolicy::from_env(),
        Arc::new(AuditMetrics::default()),
    ))
}

async fn build_rate_limiter(
    redis_url: Option<&str>,
) -> Result<Arc<dyn RateLimiter>, Box<dyn std::error::Error>> {
    if let Some(redis_url) = redis_url.filter(|value| !value.trim().is_empty()) {
        match RedisRateLimiter::connect(redis_url).await {
            Ok(limiter) => return Ok(Arc::new(limiter)),
            Err(error) => {
                if std::env::var("IDENTITY_RATE_LIMIT_REDIS_REQUIRED")
                    .ok()
                    .map(|value| {
                        matches!(value.to_ascii_lowercase().as_str(), "1" | "true" | "yes")
                    })
                    .unwrap_or(false)
                {
                    return Err(Box::new(error));
                }
                tracing::warn!(%error, "Redis rate limiter unavailable; using in-memory limiter");
            }
        }
    } else {
        tracing::warn!("REDIS_URL is not set; using in-memory identity rate limiter");
    }
    Ok(Arc::new(InMemoryRateLimiter::new()))
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
