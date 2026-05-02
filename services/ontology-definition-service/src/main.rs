//! Binary entrypoint for `ontology-definition-service`.
//!
//! Wiring (S1.6):
//!
//! 1. `AppConfig::from_env()` — pulls `DATABASE_URL` (pointing at
//!    `pg-schemas`), `PG_SCHEMA`, `NATS_URL`, etc.
//! 2. [`db::build_pool`] — Postgres pool with `search_path` pinned to
//!    `ontology_schema`. Empty `DATABASE_URL` ⇒ degrade to no-DB shell
//!    with a `tracing::warn!` (CI / dev only).
//! 3. [`schema_events::SchemaPublisher`] — JetStream client that emits
//!    `ontology.schema.v1` for downstream consumers. Empty `NATS_URL`
//!    ⇒ disabled publisher (no-op + warn).
//! 4. `build_router(state)` — minimal HTTP surface; kernel handlers
//!    bind in per-PR follow-ups.

use std::sync::Arc;

use ontology_definition_service::{
    AppState, build_router,
    config::AppConfig,
    db::build_pool,
    schema_events::{SchemaPublisher, SOURCE},
};
use tower_http::trace::TraceLayer;
use tracing_subscriber::{EnvFilter, fmt};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("ontology_definition_service=info,tower_http=info")
            }),
        )
        .init();

    let config = AppConfig::from_env().map_err(|e| {
        tracing::error!("config load failed: {e}");
        e
    })?;
    let bind_address = config.bind_address();

    let db = if config.database_url.is_empty() {
        tracing::warn!(
            "DATABASE_URL is empty; ontology-definition-service is starting without a Postgres pool. \
             Production deployments must point at pg-schemas (schema {}).",
            config.pg_schema,
        );
        None
    } else {
        let pool = build_pool(&config.database_url, &config.pg_schema).await?;
        tracing::info!(
            "connected to pg-schemas ({}); search_path={}",
            redact_url(&config.database_url),
            config.pg_schema,
        );
        Some(pool)
    };

    let publisher = if config.nats_url.is_empty() {
        tracing::warn!(
            "NATS_URL is empty; ontology.schema.v1 publishing is disabled. \
             Downstream caches (SDK gen, catalog, indexer) will not be invalidated."
        );
        SchemaPublisher::disabled()
    } else {
        let js = event_bus_control::connect(&config.nats_url).await?;
        let publisher = event_bus_control::Publisher::new(js, SOURCE);
        tracing::info!("ontology.schema.v1 publisher ready ({})", config.nats_url);
        SchemaPublisher::new(publisher)
    };

    let state = AppState {
        db,
        publisher,
        config: Arc::new(config),
    };

    let app = build_router(state).layer(TraceLayer::new_for_http());

    let listener = tokio::net::TcpListener::bind(&bind_address).await?;
    tracing::info!("ontology-definition-service listening on {bind_address}");
    axum::serve(listener, app).await?;
    Ok(())
}

/// Strip user:password from a Postgres URL for logging.
fn redact_url(url: &str) -> String {
    match url.split_once("://") {
        Some((scheme, rest)) => match rest.split_once('@') {
            Some((_creds, host)) => format!("{scheme}://***@{host}"),
            None => url.to_string(),
        },
        None => url.to_string(),
    }
}
