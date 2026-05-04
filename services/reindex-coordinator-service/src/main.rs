//! `reindex-coordinator-service` binary.
//!
//! Wires up the runtime in `lib.rs::runtime`: opens a Postgres
//! pool against `pg-runtime-config`, a Cassandra session against
//! `ontology_objects.*`, the Kafka subscriber on
//! `ontology.reindex.requested.v1` and the publisher on
//! `ontology.reindex.v1` / `ontology.reindex.completed.v1`, and
//! starts the consumer loop alongside the `:9090/metrics` +
//! `:8080/health` HTTP server.

use std::sync::Arc;

use cassandra_kernel::{ClusterConfig, SessionBuilder};
use idempotency::postgres::PgIdempotencyStore;
use reindex_coordinator_service::runtime::{
    self, CONSUMER_GROUP, Coordinator, RuntimeError, RuntimeMetrics, Throttle,
    data_bus_config_from_env, health_addr_from_env, kafka_publisher_from_env,
    metrics_addr_from_env,
};
use reindex_coordinator_service::scan::CassandraScanner;
use reindex_coordinator_service::state::JobRepo;
use sqlx::postgres::PgPoolOptions;
use std::time::Duration;
use tracing_subscriber::EnvFilter;

const PROCESSED_EVENTS_TABLE: &str = "reindex_coordinator.processed_events";

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("reindex_coordinator_service=info,tower_http=info")
            }),
        )
        .init();

    // ── Postgres (pg-runtime-config) ────────────────────────────
    let database_url =
        std::env::var("DATABASE_URL").map_err(|_| RuntimeError::MissingEnv("DATABASE_URL"))?;
    let pool = PgPoolOptions::new()
        .max_connections(parse_max_pool()?)
        .acquire_timeout(Duration::from_secs(10))
        .connect(&database_url)
        .await?;
    let jobs = Arc::new(JobRepo::new(pool.clone()));
    let idempotency = Arc::new(PgIdempotencyStore::new(pool, PROCESSED_EVENTS_TABLE));

    // ── Cassandra session ───────────────────────────────────────
    let cassandra_keyspace =
        std::env::var("CASSANDRA_KEYSPACE").unwrap_or_else(|_| "ontology_objects".to_string());
    let cluster_config = cassandra_cluster_from_env(cassandra_keyspace.clone())?;
    let session = SessionBuilder::new(cluster_config)
        .build()
        .await
        .map_err(|e| -> Box<dyn std::error::Error> { Box::new(e) })?;
    let scanner = Arc::new(CassandraScanner::new(Arc::new(session), cassandra_keyspace));

    // ── Kafka publisher + subscriber ────────────────────────────
    let publisher = Arc::new(kafka_publisher_from_env()?);
    let bus_config = data_bus_config_from_env(CONSUMER_GROUP)?;
    let subscriber = event_bus_data::KafkaSubscriber::new(&bus_config, CONSUMER_GROUP)?;

    // ── Coordinator + metrics ───────────────────────────────────
    let metrics = Arc::new(RuntimeMetrics::new());
    let coordinator = Arc::new(Coordinator {
        jobs,
        idempotency,
        scanner,
        publisher,
        metrics: Arc::clone(&metrics),
        throttle: Throttle::from_env()?,
        lineage_namespace: std::env::var("OF_OPENLINEAGE_NAMESPACE")
            .unwrap_or_else(|_| "openfoundry".to_string()),
    });

    // ── HTTP control plane (metrics + health) ───────────────────
    let metrics_for_http = Arc::clone(&metrics);
    let metrics_addr = metrics_addr_from_env(9090)?;
    tokio::spawn(async move {
        if let Err(e) = runtime::serve_http(metrics_for_http, metrics_addr).await {
            tracing::error!(error = %e, addr = %metrics_addr, "metrics HTTP server stopped");
        }
    });
    // Health is served on the same handler under /health so by
    // default we share the metrics port; expose a second listener
    // only when `HEALTH_ADDR` is explicitly configured.
    if std::env::var_os("HEALTH_ADDR").is_some() {
        let metrics_for_health = Arc::clone(&metrics);
        let health_addr = health_addr_from_env(8080)?;
        tokio::spawn(async move {
            if let Err(e) = runtime::serve_http(metrics_for_health, health_addr).await {
                tracing::error!(error = %e, addr = %health_addr, "health HTTP server stopped");
            }
        });
    }

    tracing::info!("reindex-coordinator-service starting");
    runtime::run(coordinator, subscriber).await?;
    Ok(())
}

fn parse_max_pool() -> Result<u32, RuntimeError> {
    match std::env::var("DATABASE_MAX_CONNECTIONS") {
        Err(_) => Ok(10),
        Ok(s) => s
            .trim()
            .parse::<u32>()
            .map_err(|_| RuntimeError::InvalidEnv {
                key: "DATABASE_MAX_CONNECTIONS",
                value: s,
                reason: "expected unsigned integer",
            }),
    }
}

fn cassandra_cluster_from_env(keyspace: String) -> Result<ClusterConfig, RuntimeError> {
    let raw = std::env::var("CASSANDRA_CONTACT_POINTS")
        .map_err(|_| RuntimeError::MissingEnv("CASSANDRA_CONTACT_POINTS"))?;
    let contact_points: Vec<String> = raw
        .split(',')
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect();
    if contact_points.is_empty() {
        return Err(RuntimeError::InvalidEnv {
            key: "CASSANDRA_CONTACT_POINTS",
            value: raw,
            reason: "expected comma-separated list of host:port pairs",
        });
    }
    let local_datacenter =
        std::env::var("CASSANDRA_LOCAL_DC").unwrap_or_else(|_| "dc1".to_string());
    let username = std::env::var("CASSANDRA_USERNAME")
        .ok()
        .filter(|s| !s.is_empty());
    let password = std::env::var("CASSANDRA_PASSWORD")
        .ok()
        .filter(|s| !s.is_empty());
    let connect_timeout = Duration::from_secs(parse_u64_env("CASSANDRA_CONNECT_TIMEOUT_SECS", 10)?);
    let request_timeout = Duration::from_secs(parse_u64_env("CASSANDRA_REQUEST_TIMEOUT_SECS", 30)?);
    Ok(ClusterConfig {
        contact_points,
        local_datacenter,
        username,
        password,
        keyspace: Some(keyspace),
        enable_tracing: false,
        connect_timeout,
        request_timeout,
    })
}

fn parse_u64_env(key: &'static str, default: u64) -> Result<u64, RuntimeError> {
    match std::env::var(key) {
        Err(_) => Ok(default),
        Ok(s) => s
            .trim()
            .parse::<u64>()
            .map_err(|_| RuntimeError::InvalidEnv {
                key,
                value: s,
                reason: "expected unsigned integer",
            }),
    }
}
