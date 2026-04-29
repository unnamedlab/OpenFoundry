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
        registry.insert(Arc::new(KafkaUnavailableBackend::new()));
        tracing::warn!(
            "Kafka backend is configured but the real rdkafka integration is not yet enabled (PR 2). Publishes will return UNAVAILABLE."
        );
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
