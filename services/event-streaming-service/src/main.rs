//! `event-streaming-service` entrypoint.
//!
//! Boots the routing table from `topic-routes.yaml`, registers the configured
//! backends, and starts two listeners:
//!
//! * the gRPC server (`Publish` / `Subscribe`) on `grpc_port`
//! * an HTTP side router exposing `/healthz` and `/metrics` on `admin_port`

use std::net::SocketAddr;
use std::sync::Arc;

use axum::{Router, extract::State, http::StatusCode, response::IntoResponse, routing::get};
use tracing_subscriber::EnvFilter;

use event_streaming_service::app_config::AppConfig;
use event_streaming_service::backends::{
    BackendRegistry, KafkaUnavailableBackend, NatsBackend,
};
#[cfg(feature = "kafka-rdkafka")]
use event_streaming_service::backends::RdKafkaBackend;
use event_streaming_service::grpc::EventRouterService;
use event_streaming_service::metrics::Metrics;
use event_streaming_service::router::{BackendId, RouterConfig};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("event_streaming_service=info,tonic=info")),
        )
        .init();

    let cfg = AppConfig::from_env()?;
    let routes = RouterConfig::load(&cfg.routes_file)?;
    let table = Arc::new(routes.compile()?);

    // Build the backend registry from the routing config. Backends that are
    // not referenced by any route are simply not constructed, so the service
    // can run with NATS-only or Kafka-only configurations.
    let mut registry = BackendRegistry::new();
    if let Some(nats_cfg) = &routes.backends.nats {
        let backend = NatsBackend::connect(&nats_cfg.url).await?;
        registry.insert(Arc::new(backend));
        tracing::info!(url = %nats_cfg.url, "NATS backend connected");
    }
    if let Some(_kafka_cfg) = &routes.backends.kafka {
        match build_kafka_backend(_kafka_cfg) {
            Ok(backend) => {
                registry.insert(backend);
                tracing::info!("Kafka backend connected via rdkafka");
            }
            Err(reason) => {
                registry.insert(Arc::new(KafkaUnavailableBackend::new()));
                tracing::warn!(
                    reason = %reason,
                    "Kafka backend is configured but the real rdkafka integration is not active. Publishes will return UNAVAILABLE."
                );
            }
        }
    }
    let _ = BackendId::Kafka; // keep the symbol referenced for clarity

    let metrics = Arc::new(Metrics::new());
    let backends = Arc::new(registry);

    let grpc_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.grpc_port).parse()?;
    let admin_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.admin_port).parse()?;

    tracing::info!(%grpc_addr, "starting EventRouter gRPC server");
    tracing::info!(%admin_addr, "starting admin side router (/healthz, /metrics)");

    let svc = EventRouterService::new(table, backends, Arc::clone(&metrics));
    let grpc_server = tonic::transport::Server::builder()
        .add_service(svc.into_server())
        .serve(grpc_addr);

    let admin_app = Router::new()
        .route("/healthz", get(healthz))
        .route("/health", get(healthz))
        .route("/metrics", get(metrics_handler))
        .with_state(metrics);
    let admin_listener = tokio::net::TcpListener::bind(admin_addr).await?;
    let admin_server = axum::serve(admin_listener, admin_app);

    tokio::select! {
        result = grpc_server => result?,
        result = admin_server => result?,
    }
    Ok(())
}

async fn healthz() -> (StatusCode, &'static str) {
    (StatusCode::OK, "ok")
}

/// Build a real Kafka backend when the `kafka-rdkafka` Cargo feature is
/// compiled in **and** `KAFKA_BOOTSTRAP_SERVERS` is set. Otherwise return an
/// `Err` so `main` can fall back to the unavailable stub. The router-level
/// configuration (`backends.kafka.brokers`) is intentionally ignored when the
/// env var is present so operators can override the routing file without
/// editing it (e.g. in dev/CI).
#[cfg(feature = "kafka-rdkafka")]
fn build_kafka_backend(
    kafka_cfg: &event_streaming_service::router::config::KafkaBackendConfig,
) -> Result<Arc<dyn event_streaming_service::backends::Backend>, String> {
    use event_bus_data::config::{DataBusConfig, ServicePrincipal};
    let env_brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS").ok();
    let brokers = match env_brokers {
        Some(v) if !v.is_empty() => v,
        _ if !kafka_cfg.brokers.is_empty() => kafka_cfg.brokers.join(","),
        _ => return Err("no Kafka brokers configured (KAFKA_BOOTSTRAP_SERVERS unset and routing file has empty `brokers`)".to_string()),
    };
    let service = kafka_cfg
        .client_id
        .clone()
        .or_else(|| std::env::var("KAFKA_CLIENT_ID").ok())
        .unwrap_or_else(|| "event-streaming-service".to_string());
    let principal = match (
        std::env::var("KAFKA_SASL_USERNAME").ok(),
        std::env::var("KAFKA_SASL_PASSWORD").ok(),
    ) {
        (Some(_), Some(password)) => ServicePrincipal::scram_sha_512(service.clone(), password),
        _ => ServicePrincipal::insecure_dev(service.clone()),
    };
    let cfg = DataBusConfig::new(brokers, principal);
    let group_prefix = format!("{service}-router");
    RdKafkaBackend::new(cfg, group_prefix)
        .map(|b| Arc::new(b) as Arc<dyn event_streaming_service::backends::Backend>)
        .map_err(|e| e.to_string())
}

#[cfg(not(feature = "kafka-rdkafka"))]
fn build_kafka_backend(
    _kafka_cfg: &event_streaming_service::router::config::KafkaBackendConfig,
) -> Result<Arc<dyn event_streaming_service::backends::Backend>, String> {
    Err("event-streaming-service was built without the `kafka-rdkafka` feature".to_string())
}

async fn metrics_handler(State(metrics): State<Arc<Metrics>>) -> impl IntoResponse {
    match metrics.render() {
        Ok(body) => (
            StatusCode::OK,
            [("content-type", "text/plain; version=0.0.4")],
            body,
        )
            .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("metrics render error: {e}"),
        )
            .into_response(),
    }
}

// `storage` is exercised by integration tests and by future stream
// operators; declaring it here keeps the legacy/iceberg writer modules in
// the build graph until the streaming runtime grows operators that wire
// them up at startup.
#[allow(dead_code, unused_imports)]
mod storage;
