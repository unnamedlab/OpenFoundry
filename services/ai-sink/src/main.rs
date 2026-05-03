//! `ai-sink` binary — Kafka → Iceberg.
//!
//! Stream **S5.3** of the Cassandra/Foundry parity plan. Consumes
//! `ai.events.v1` and routes each event to the appropriate
//! `of_ai.{prompts,responses,evaluations,traces}` table.

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .json()
        .init();

    let config = ai_sink::runtime::RuntimeConfig::from_env()?;
    let subscriber =
        event_bus_data::KafkaSubscriber::new(&config.data_bus, ai_sink::CONSUMER_GROUP)?;
    let tables = ai_sink::runtime::load_tables(&config).await?;

    tracing::info!(
        topic = ai_sink::SOURCE_TOPIC,
        namespace = ai_sink::iceberg_target::NAMESPACE,
        "ai-sink starting Kafka -> Iceberg runtime"
    );
    ai_sink::runtime::run(subscriber, tables, config.batch_policy).await?;
    Ok(())
}
