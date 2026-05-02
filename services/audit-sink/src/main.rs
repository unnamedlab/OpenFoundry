//! `audit-sink` binary — Kafka → Iceberg.
//!
//! Substrate stub. Real Iceberg writer lands in S5.1.b.

fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .json()
        .init();

    tracing::info!(
        topic = audit_sink::SOURCE_TOPIC,
        target = format!(
            "{}.{}.{}",
            audit_sink::iceberg_target::CATALOG,
            audit_sink::iceberg_target::NAMESPACE,
            audit_sink::iceberg_target::TABLE
        ),
        "audit-sink starting (substrate stub)"
    );
}
