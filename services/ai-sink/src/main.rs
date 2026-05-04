//! `ai-sink` binary — Kafka → Iceberg.
//!
//! Stream **S5.3** of the Cassandra/Foundry parity plan. Consumes
//! `ai.events.v1` and routes each event to the appropriate
//! `of_ai.{prompts,responses,evaluations,traces}` table.

use std::sync::Arc;

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
    let metrics = Arc::new(ai_sink::runtime::RuntimeMetrics::new());
    let metrics_addr = ai_sink::runtime::metrics_addr_from_env(9090)?;
    {
        let metrics = Arc::clone(&metrics);
        tokio::spawn(async move {
            if let Err(error) = ai_sink::runtime::serve_metrics(metrics, metrics_addr).await {
                tracing::error!(%error, "ai-sink metrics endpoint stopped");
            }
        });
    }
    let subscriber =
        event_bus_data::KafkaSubscriber::new(&config.data_bus, ai_sink::CONSUMER_GROUP)?;
    let tables = ai_sink::runtime::load_tables(&config).await?;

    tracing::info!(
        topic = ai_sink::SOURCE_TOPIC,
        namespace = ai_sink::iceberg_target::NAMESPACE,
        "ai-sink starting Kafka -> Iceberg runtime"
    );
    ai_sink::runtime::run_with_metrics(subscriber, tables, config.batch_policy, Some(metrics))
        .await?;
    Ok(())
}
