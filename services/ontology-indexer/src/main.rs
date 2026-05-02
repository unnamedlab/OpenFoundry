//! `ontology-indexer` binary — Kafka → SearchBackend.
//!
//! Substrate stub. The real consumer loop lands in a follow-up PR
//! that wires `event-bus-data::DataSubscriber::recv` against the
//! decoder in [`ontology_indexer`] and the backend selected via
//! [`ontology_indexer::BackendKind::from_env`]. This main keeps the
//! binary buildable so the deployment manifest in
//! `infra/k8s/helm/open-foundry/templates/` can ship in parallel.

fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .json()
        .init();

    let backend = ontology_indexer::BackendKind::from_env(
        std::env::var("SEARCH_BACKEND").ok().as_deref(),
    );
    tracing::info!(?backend, "ontology-indexer starting (substrate stub)");

    // The wiring of `DataSubscriber` + `SearchBackend::index` lives
    // in a follow-up PR. Until then the binary exits cleanly so
    // the readiness probe stays honest in non-prod environments.
}
