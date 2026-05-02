//! `audit-sink` binary — Kafka → Iceberg.

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .json()
        .init();

    let config = audit_sink::runtime::RuntimeConfig::from_env()?;
    let subscriber =
        event_bus_data::KafkaSubscriber::new(&config.data_bus, audit_sink::CONSUMER_GROUP)?;
    let table = audit_sink::runtime::load_table(&config).await?;

    tracing::info!(
        topic = audit_sink::SOURCE_TOPIC,
        target = format!(
            "{}.{}.{}",
            audit_sink::iceberg_target::CATALOG,
            audit_sink::iceberg_target::NAMESPACE,
            audit_sink::iceberg_target::TABLE
        ),
        "audit-sink starting Kafka -> Iceberg runtime"
    );
    audit_sink::runtime::run(subscriber, table, config.batch_policy).await?;
    Ok(())
}
