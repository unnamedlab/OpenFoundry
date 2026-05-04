//! `schedules-tick` — single-shot CLI that runs one
//! [`Scheduler::tick`](event_scheduler::Scheduler::tick) and exits.
//!
//! Intended deployment: a Kubernetes `CronJob` running every minute.
//! Each pod boots, connects to Postgres + Kafka, fires every due
//! schedule once, and exits with code 0 on success.
//!
//! Environment:
//!
//! * `DATABASE_URL` — required, e.g. `postgres://user:pw@scheduler-db/foundry`.
//! * `KAFKA_BOOTSTRAP_SERVERS` — required, comma-separated brokers.
//! * `KAFKA_SASL_USERNAME` / `KAFKA_SASL_PASSWORD` /
//!   `KAFKA_SASL_MECHANISM` / `KAFKA_SECURITY_PROTOCOL` — optional;
//!   see `event_bus_data::KafkaPublisher::from_env`.
//! * `RUST_LOG` — log filter (default: `info`).

use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context, Result};
use chrono::Utc;
use event_scheduler::Scheduler;
use event_scheduler::event_bus_data::KafkaPublisher;
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

const SERVICE_NAME: &str = "schedules-tick";

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .with_target(false)
        .json()
        .init();

    let database_url = std::env::var("DATABASE_URL")
        .context("DATABASE_URL must be set (Postgres connection URL)")?;

    let pg = PgPoolOptions::new()
        .max_connections(4)
        .acquire_timeout(Duration::from_secs(10))
        .connect(&database_url)
        .await
        .context("failed to connect to Postgres")?;

    let publisher =
        KafkaPublisher::from_env(SERVICE_NAME).context("failed to build Kafka publisher")?;

    let scheduler = Scheduler::new(pg, Arc::new(publisher));

    let now = Utc::now();
    let fired = scheduler.tick(now).await.context("scheduler tick failed")?;

    tracing::info!(fired, %now, "schedules-tick completed");
    println!("{fired}");
    Ok(())
}
