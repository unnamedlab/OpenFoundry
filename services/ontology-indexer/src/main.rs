//! `ontology-indexer` binary — Kafka → SearchBackend.
//!
//! Consumes ontology events from Kafka and applies them to the configured
//! search backend selected by `SEARCH_BACKEND` / `SEARCH_ENDPOINT`.

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .json()
        .init();

    let backend_kind =
        ontology_indexer::BackendKind::from_env(std::env::var("SEARCH_BACKEND").ok().as_deref());
    let backend = search_abstraction::search_backend_from_env()?;
    let data_bus = ontology_indexer::runtime::data_bus_config_from_env("ontology-indexer")?;
    let subscriber =
        event_bus_data::KafkaSubscriber::new(&data_bus, ontology_indexer::runtime::CONSUMER_GROUP)?;

    tracing::info!(
        ?backend_kind,
        "ontology-indexer starting Kafka -> SearchBackend runtime"
    );
    ontology_indexer::runtime::run(subscriber, backend).await?;
    Ok(())
}
