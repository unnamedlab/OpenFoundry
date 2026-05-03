//! `ontology-query-service` binary entry point.
//!
//! Owns the read surface of the Foundry-parity ontology plane
//! ([migration plan §S1.5](../../../docs/architecture/migration-plan-cassandra-foundry-parity.md)).
//!
//! Wiring summary:
//!
//! 1. Build a [`CassandraObjectStore`](cassandra_kernel::repos::CassandraObjectStore)
//!    when `CASSANDRA_CONTACT_POINTS` is set, fall back to the
//!    in-memory store otherwise (smoke tests / dev).
//! 2. Wrap it in a [`CachingObjectStore`](ontology_query_service::cache::CachingObjectStore)
//!    (S1.5.a — moka, 100k entries / 30 s TTL by default).
//! 3. Spawn the [invalidation subscriber](ontology_query_service::invalidation)
//!    (S1.5.b — NATS subject `ontology.write.v1`, bridged from the
//!    Debezium Kafka topic of the same name).
//! 4. Mount the read router from
//!    [`build_router`](ontology_query_service::build_router) and
//!    expose `/health`.
//!
//! Per S1.5.e, this binary applies **no** `sqlx::migrate!` — the
//! legacy projections that lived under `migrations/` were archived
//! to `docs/architecture/legacy-migrations/ontology-query-service/`
//! and the read service no longer needs `sqlx`.

use std::net::SocketAddr;
use std::sync::Arc;

use axum::{Router, routing::get};
use cassandra_kernel::{ClusterConfig, SessionBuilder, repos::CassandraObjectStore};
use ontology_query_service::cache::CachingObjectStore;
use ontology_query_service::config::AppConfig;
use ontology_query_service::{QueryState, build_router, invalidation};
use storage_abstraction::repositories::ObjectStore;
use tower_http::trace::TraceLayer;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("ontology_query_service=info,tower_http=info")),
        )
        .init();

    let app_config = AppConfig::from_env()?;

    let inner = build_inner_store(&app_config).await?;
    let cache = Arc::new(CachingObjectStore::with_config(
        inner,
        app_config.cache_capacity_or_default(),
        app_config.cache_ttl(),
    ));

    // Spawn the cross-replica invalidation subscriber (S1.5.b). On
    // dev / smoke-test runs `nats_url` may be empty; we degrade to a
    // warn so the service still boots.
    if app_config.nats_url.trim().is_empty() {
        tracing::warn!(
            "NATS_URL not set — running without cross-replica cache invalidation. \
             Reads will be served from cache until TTL expiry (S1.5.b)."
        );
    } else {
        match invalidation::spawn(app_config.nats_url.clone(), cache.clone()).await {
            Ok(_handle) => {}
            Err(error) => {
                tracing::error!(?error, "failed to subscribe to invalidation bus");
                cache.invalidate_all();
            }
        }
    }

    let state = QueryState {
        objects: cache.clone() as Arc<dyn ObjectStore>,
    };

    let app = Router::new()
        .merge(build_router(state))
        .route("/health", get(|| async { "ok" }))
        .layer(TraceLayer::new_for_http());

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting ontology-query-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn build_inner_store(
    cfg: &AppConfig,
) -> Result<Arc<dyn ObjectStore>, Box<dyn std::error::Error>> {
    if cfg.cassandra_contact_points.trim().is_empty() {
        tracing::warn!(
            "CASSANDRA_CONTACT_POINTS not set — falling back to in-memory ObjectStore. \
             Production deployments MUST set this variable (S1.5)."
        );
        return Ok(Arc::new(
            storage_abstraction::repositories::noop::InMemoryObjectStore::default(),
        ));
    }

    let cluster = ClusterConfig {
        contact_points: cfg
            .cassandra_contact_points
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect(),
        local_datacenter: cfg.cassandra_local_dc.clone(),
        ..ClusterConfig::dev_local()
    };
    let session = Arc::new(SessionBuilder::new(cluster).build().await?);
    let store = CassandraObjectStore::new(session);
    store.warm_up().await?;
    Ok(Arc::new(store))
}
