//! `iceberg-catalog-service` binary.
//!
//! Boots tracing, Postgres, the shared HTTP client used to talk to
//! `oauth-integration-service` / `identity-federation-service`, builds
//! the [`AppState`] and hands it to the router exposed by
//! [`iceberg_catalog_service::build_router`].
//!
//! The service speaks the Apache Iceberg REST Catalog OpenAPI spec
//! (`/iceberg/v1/...`) plus the Foundry-internal admin surface
//! consumed by the `apps/web` UI.
//!
//! ## Environment
//!
//! | Variable | Purpose |
//! |---|---|
//! | `DATABASE_URL`                            | Postgres URL (required) |
//! | `ICEBERG_CATALOG_HOST`                    | Bind host (default: `0.0.0.0`) |
//! | `ICEBERG_CATALOG_PORT`                    | Bind port (default: `8197`) |
//! | `ICEBERG_CATALOG_WAREHOUSE_URI`           | Warehouse URI returned in `/iceberg/v1/config` |
//! | `ICEBERG_CATALOG_JWT_ISSUER`              | Iss claim for service-issued JWTs |
//! | `ICEBERG_CATALOG_JWT_AUDIENCE`            | Aud claim for service-issued JWTs |
//! | `ICEBERG_CATALOG_TOKEN_TTL_SECS`          | Access-token TTL (default: 3600) |
//! | `ICEBERG_CATALOG_LONG_LIVED_TOKEN_TTL_SECS` | API-token TTL (default: 90 days) |
//! | `IDENTITY_FEDERATION_URL`                 | Identity service base URL |
//! | `OAUTH_INTEGRATION_URL`                   | OAuth integration base URL |
//! | `OPENFOUNDRY_JWT_SECRET` / `JWT_SECRET`   | Shared HMAC secret |
//!
//! ## Routes
//!
//! See `iceberg_catalog_service::build_router` for the full list. In
//! short: `/iceberg/v1/...` is the spec surface, `/api/v1/iceberg-tables/...`
//! is the UI surface, `/health` and `/metrics` are operational.

use std::net::SocketAddr;

use auth_middleware::jwt::JwtConfig;
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

use iceberg_catalog_service::config::AppConfig;
use iceberg_catalog_service::{AppState, IcebergState, build_router, metrics};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // ─── Tracing & metrics bootstrap ────────────────────────────────────
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("iceberg_catalog_service=info,tower_http=info,axum=info")
        }))
        .init();

    // Touch every Lazy metric so the registry is populated before the
    // first scrape (Prometheus would otherwise see them only after the
    // first request increments them).
    metrics::record_rest_request("INIT", "/iceberg/v1", 0);
    metrics::OAUTH_TOKENS_ISSUED
        .with_label_values(&["init"])
        .reset();
    metrics::TABLES_TOTAL
        .with_label_values(&["2"])
        .set(0);

    // ─── Configuration ──────────────────────────────────────────────────
    let config = AppConfig::from_env()?;
    tracing::info!(
        host = %config.host,
        port = config.port,
        warehouse = %config.warehouse_uri,
        "iceberg-catalog-service config loaded"
    );

    // ─── Postgres pool + migrations ─────────────────────────────────────
    let db = PgPoolOptions::new()
        .max_connections(20)
        .connect(&config.database_url)
        .await?;

    if std::env::var("ICEBERG_CATALOG_RUN_MIGRATIONS")
        .ok()
        .map(|v| matches!(v.as_str(), "1" | "true" | "yes"))
        .unwrap_or(true)
    {
        sqlx::migrate!("./migrations").run(&db).await?;
        tracing::info!("iceberg-catalog migrations applied");
    } else {
        tracing::warn!("skipping iceberg-catalog migrations (ICEBERG_CATALOG_RUN_MIGRATIONS=0)");
    }

    // ─── Shared HTTP client for outbound calls ──────────────────────────
    let http = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()?;

    // ─── App state ──────────────────────────────────────────────────────
    let jwt_config = JwtConfig::new(&config.jwt_secret)
        .with_issuer(&config.jwt_issuer)
        .with_audience(&config.jwt_audience)
        .with_env_defaults();

    let authz_engine = iceberg_catalog_service::authz::bootstrap_engine().await;
    let default_tenant =
        std::env::var("ICEBERG_CATALOG_DEFAULT_TENANT").unwrap_or_else(|_| "default".to_string());

    let state = AppState::new(IcebergState {
        db,
        jwt_config,
        warehouse_uri: config.warehouse_uri.clone(),
        identity_federation_url: config.identity_federation_url.clone(),
        oauth_integration_url: config.oauth_integration_url.clone(),
        default_token_ttl_secs: config.default_token_ttl_secs,
        long_lived_token_ttl_secs: config.long_lived_token_ttl_secs,
        jwt_issuer: config.jwt_issuer.clone(),
        jwt_audience: config.jwt_audience.clone(),
        http,
        authz: authz_engine,
        default_tenant,
    });

    // ─── Tower middleware stack ─────────────────────────────────────────
    use tower_http::cors::{Any, CorsLayer};
    use tower_http::request_id::{MakeRequestUuid, PropagateRequestIdLayer, SetRequestIdLayer};
    use tower_http::trace::TraceLayer;

    let cors = CorsLayer::new()
        .allow_methods(Any)
        .allow_origin(Any)
        .allow_headers(Any);

    let request_id_header = axum::http::HeaderName::from_static("x-request-id");

    let app = build_router(state)
        .layer(SetRequestIdLayer::new(request_id_header.clone(), MakeRequestUuid))
        .layer(PropagateRequestIdLayer::new(request_id_header))
        .layer(TraceLayer::new_for_http())
        .layer(cors);

    // ─── Bind and serve ─────────────────────────────────────────────────
    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting iceberg-catalog-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
