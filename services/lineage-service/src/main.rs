//! `lineage-service` binary.
//!
//! Supports two runtime modes:
//! 1. Kafka `lineage.events.v1` → Iceberg `of.lineage.*`
//! 2. Minimal HTTP `/health` endpoint for environments that only
//!    need service discovery while the query surface is disabled.

mod config;

use std::net::SocketAddr;

use axum::{Router, routing::get};
use config::AppConfig;
use tracing_subscriber::EnvFilter;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum RuntimeMode {
    KafkaToIceberg,
    HttpHealth,
}

impl RuntimeMode {
    fn from_env() -> Self {
        match std::env::var("LINEAGE_RUNTIME_MODE")
            .ok()
            .unwrap_or_default()
            .trim()
            .to_ascii_lowercase()
            .as_str()
        {
            "kafka" | "kafka_to_iceberg" | "iceberg" => Self::KafkaToIceberg,
            "http" | "http_health" => Self::HttpHealth,
            _ if std::env::var("ICEBERG_CATALOG_URL").is_ok()
                && std::env::var("KAFKA_BOOTSTRAP_SERVERS").is_ok() =>
            {
                Self::KafkaToIceberg
            }
            _ => Self::HttpHealth,
        }
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("lineage_service=info,tower_http=info")),
        )
        .init();

    match RuntimeMode::from_env() {
        RuntimeMode::KafkaToIceberg => {
            let config = lineage_service::runtime::RuntimeConfig::from_env()?;
            let subscriber = event_bus_data::KafkaSubscriber::new(
                &config.data_bus,
                lineage_service::kafka_to_iceberg::CONSUMER_GROUP,
            )?;
            let tables = lineage_service::runtime::load_tables(&config).await?;

            tracing::info!(
                topic = lineage_service::kafka_to_iceberg::SOURCE_TOPIC,
                namespace = lineage_service::kafka_to_iceberg::iceberg_target::NAMESPACE,
                "starting lineage-service Kafka -> Iceberg runtime"
            );
            lineage_service::runtime::run(subscriber, tables, config.batch_policy).await?;
        }
        RuntimeMode::HttpHealth => {
            let config = AppConfig::from_env()?;
            let app = Router::new().route("/health", get(|| async { "ok" }));

            let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
            tracing::info!(%addr, "starting lineage-service health endpoint");
            let listener = tokio::net::TcpListener::bind(addr).await?;
            axum::serve(listener, app).await?;
        }
    }

    Ok(())
}
