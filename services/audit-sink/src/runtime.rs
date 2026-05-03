//! Runtime wiring for `audit-sink` (Kafka → Iceberg writer).

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use arrow_array::{FixedSizeBinaryArray, RecordBatch, StringArray, TimestampMicrosecondArray};
use arrow_schema::{ArrowError, DataType, Field, Schema, TimeUnit};
use event_bus_data::{
    CommitError, DataBusConfig, DataMessage, DataSubscriber, ServicePrincipal, SubscribeError,
};
use storage_abstraction::iceberg::{IcebergError, IcebergTable};
use thiserror::Error;

use crate::{
    AuditEnvelope, BatchPolicy, CONSUMER_GROUP, SOURCE_TOPIC, decode, iceberg_schema,
    iceberg_target,
};

/// Runtime configuration resolved at process startup.
#[derive(Debug, Clone)]
pub struct RuntimeConfig {
    pub data_bus: DataBusConfig,
    pub catalog_url: String,
    pub warehouse: Option<String>,
    pub batch_policy: BatchPolicy,
}

#[derive(Debug, Error)]
pub enum RuntimeError {
    #[error("required environment variable {0} is not set")]
    MissingEnv(&'static str),
    #[error("invalid environment variable {key}={value:?}: {reason}")]
    InvalidEnv {
        key: &'static str,
        value: String,
        reason: &'static str,
    },
    #[error("kafka subscribe/receive failed: {0}")]
    Subscribe(#[from] SubscribeError),
    #[error("kafka offset commit failed: {0}")]
    Commit(#[from] CommitError),
    #[error("invalid audit event JSON: {0}")]
    Json(#[from] serde_json::Error),
    #[error("arrow batch build failed: {0}")]
    Arrow(#[from] ArrowError),
    #[error("iceberg write failed: {0}")]
    Iceberg(#[from] IcebergError),
}

impl RuntimeConfig {
    pub fn from_env() -> Result<Self, RuntimeError> {
        let catalog_url = non_empty_env("ICEBERG_CATALOG_URL")
            .map_err(|_| RuntimeError::MissingEnv("ICEBERG_CATALOG_URL"))?;
        Ok(Self {
            data_bus: data_bus_config_from_env(CONSUMER_GROUP)?,
            catalog_url,
            warehouse: non_empty_env("ICEBERG_WAREHOUSE").ok(),
            batch_policy: batch_policy_from_env()?,
        })
    }
}

/// Build the Kafka data-bus config from the standard OpenFoundry env vars.
pub fn data_bus_config_from_env(service_name: &str) -> Result<DataBusConfig, RuntimeError> {
    let brokers = non_empty_env("KAFKA_BOOTSTRAP_SERVERS")
        .map_err(|_| RuntimeError::MissingEnv("KAFKA_BOOTSTRAP_SERVERS"))?;
    let service = non_empty_env("KAFKA_SASL_USERNAME")
        .or_else(|_| non_empty_env("KAFKA_CLIENT_ID"))
        .unwrap_or_else(|_| service_name.to_string());

    let mut principal = match non_empty_env("KAFKA_SASL_PASSWORD") {
        Ok(password) => ServicePrincipal::scram_sha_512(service, password),
        Err(_) => ServicePrincipal::insecure_dev(service),
    };

    if let Ok(mechanism) = non_empty_env("KAFKA_SASL_MECHANISM") {
        principal.mechanism = mechanism;
    }
    if let Ok(protocol) = non_empty_env("KAFKA_SECURITY_PROTOCOL") {
        principal.security_protocol = protocol;
    }

    Ok(DataBusConfig::new(brokers, principal))
}

fn non_empty_env(key: &'static str) -> Result<String, RuntimeError> {
    std::env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .ok_or(RuntimeError::MissingEnv(key))
}

fn batch_policy_from_env() -> Result<BatchPolicy, RuntimeError> {
    let mut policy = BatchPolicy::PLAN_DEFAULT;
    if let Ok(value) = non_empty_env("AUDIT_SINK_BATCH_MAX_RECORDS") {
        policy.max_records = parse_usize_env("AUDIT_SINK_BATCH_MAX_RECORDS", value)?;
    }
    if let Ok(value) = non_empty_env("AUDIT_SINK_BATCH_MAX_WAIT_SECONDS") {
        let seconds = parse_u64_env("AUDIT_SINK_BATCH_MAX_WAIT_SECONDS", value)?;
        policy.max_wait = std::time::Duration::from_secs(seconds);
    }
    Ok(policy)
}

fn parse_usize_env(key: &'static str, value: String) -> Result<usize, RuntimeError> {
    value
        .parse::<usize>()
        .map_err(|_| RuntimeError::InvalidEnv {
            key,
            value,
            reason: "expected positive integer",
        })
}

fn parse_u64_env(key: &'static str, value: String) -> Result<u64, RuntimeError> {
    value.parse::<u64>().map_err(|_| RuntimeError::InvalidEnv {
        key,
        value,
        reason: "expected positive integer",
    })
}

pub async fn load_table(config: &RuntimeConfig) -> Result<IcebergTable, RuntimeError> {
    let namespace = [iceberg_target::NAMESPACE];
    match &config.warehouse {
        Some(warehouse) => Ok(IcebergTable::load_table_with_warehouse(
            &config.catalog_url,
            warehouse,
            &namespace,
            iceberg_target::TABLE,
        )
        .await?),
        None => {
            Ok(
                IcebergTable::load_table(&config.catalog_url, &namespace, iceberg_target::TABLE)
                    .await?,
            )
        }
    }
}

/// Subscribe and run the Kafka -> Iceberg batch writer.
pub async fn run<S>(
    subscriber: S,
    mut table: IcebergTable,
    batch_policy: BatchPolicy,
) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    subscriber.subscribe(&[SOURCE_TOPIC])?;
    tracing::info!(
        group = CONSUMER_GROUP,
        topic = SOURCE_TOPIC,
        max_records = batch_policy.max_records,
        max_wait_seconds = batch_policy.max_wait.as_secs(),
        "audit-sink consumer loop started"
    );

    let mut batch = Vec::with_capacity(batch_policy.max_records.min(4096));
    let mut first_record_at = Instant::now();

    loop {
        if !batch.is_empty() && batch_policy.should_flush(batch.len(), first_record_at.elapsed()) {
            flush_batch(&subscriber, &mut table, &mut batch).await?;
            first_record_at = Instant::now();
            continue;
        }

        let message = if batch.is_empty() {
            subscriber.recv().await?
        } else {
            let remaining = batch_policy
                .max_wait
                .saturating_sub(first_record_at.elapsed());
            match tokio::time::timeout(remaining, subscriber.recv()).await {
                Ok(result) => result?,
                Err(_) => {
                    flush_batch(&subscriber, &mut table, &mut batch).await?;
                    first_record_at = Instant::now();
                    continue;
                }
            }
        };

        let Some(payload) = message.payload() else {
            tracing::warn!(
                partition = message.partition(),
                offset = message.offset(),
                "audit-sink skipping record without payload"
            );
            subscriber.commit(&message)?;
            continue;
        };

        match decode(payload) {
            Ok(envelope) => {
                if batch.is_empty() {
                    first_record_at = Instant::now();
                }
                batch.push(PendingRecord { envelope, message });
            }
            Err(error) => {
                tracing::warn!(
                    partition = message.partition(),
                    offset = message.offset(),
                    %error,
                    "audit-sink skipping malformed audit event"
                );
                subscriber.commit(&message)?;
            }
        }
    }
}

struct PendingRecord {
    envelope: AuditEnvelope,
    message: DataMessage,
}

async fn flush_batch<S>(
    subscriber: &S,
    table: &mut IcebergTable,
    batch: &mut Vec<PendingRecord>,
) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    if batch.is_empty() {
        return Ok(());
    }

    let started = Instant::now();
    let events: Vec<AuditEnvelope> = batch.iter().map(|record| record.envelope.clone()).collect();
    let rows = events.len();
    let record_batch = events_to_record_batch(&events)?;
    table.append_record_batches(vec![record_batch]).await?;
    for record in batch.iter() {
        subscriber.commit(&record.message)?;
    }
    batch.clear();

    tracing::info!(
        rows,
        elapsed_ms = started.elapsed().as_millis(),
        target = format!(
            "{}.{}.{}",
            iceberg_target::CATALOG,
            iceberg_target::NAMESPACE,
            iceberg_target::TABLE
        ),
        "audit events committed to Iceberg"
    );
    Ok(())
}

/// Convert decoded audit events to the Arrow schema expected by Iceberg.
pub fn events_to_record_batch(events: &[AuditEnvelope]) -> Result<RecordBatch, RuntimeError> {
    let event_ids: Vec<[u8; 16]> = events
        .iter()
        .map(|event| *event.event_id.as_bytes())
        .collect();
    let at_values: Vec<i64> = events.iter().map(|event| event.at).collect();
    let correlation_ids: Vec<Option<String>> = events
        .iter()
        .map(|event| event.correlation_id.clone())
        .collect();
    let kinds: Vec<String> = events.iter().map(|event| event.kind.clone()).collect();
    let payloads: Result<Vec<String>, serde_json::Error> = events
        .iter()
        .map(|event| serde_json::to_string(&event.payload))
        .collect();

    Ok(RecordBatch::try_new(
        Arc::new(audit_arrow_schema()),
        vec![
            Arc::new(FixedSizeBinaryArray::try_from_iter(event_ids.into_iter())?),
            Arc::new(TimestampMicrosecondArray::from(at_values).with_timezone_utc()),
            Arc::new(StringArray::from(correlation_ids)),
            Arc::new(StringArray::from(kinds)),
            Arc::new(StringArray::from(payloads?)),
        ],
    )?)
}

fn audit_arrow_schema() -> Schema {
    Schema::new(vec![
        arrow_field(
            iceberg_schema::fields::EVENT_ID,
            DataType::FixedSizeBinary(16),
            false,
            iceberg_schema::field_ids::EVENT_ID,
        ),
        arrow_field(
            iceberg_schema::fields::AT,
            DataType::Timestamp(TimeUnit::Microsecond, Some("UTC".into())),
            false,
            iceberg_schema::field_ids::AT,
        ),
        arrow_field(
            iceberg_schema::fields::CORRELATION_ID,
            DataType::Utf8,
            true,
            iceberg_schema::field_ids::CORRELATION_ID,
        ),
        arrow_field(
            iceberg_schema::fields::KIND,
            DataType::Utf8,
            false,
            iceberg_schema::field_ids::KIND,
        ),
        arrow_field(
            iceberg_schema::fields::PAYLOAD,
            DataType::Utf8,
            false,
            iceberg_schema::field_ids::PAYLOAD,
        ),
    ])
}

fn arrow_field(name: &'static str, data_type: DataType, nullable: bool, field_id: i32) -> Field {
    Field::new(name, data_type, nullable).with_metadata(HashMap::from([(
        "PARQUET:field_id".to_string(),
        field_id.to_string(),
    )]))
}

/// Prometheus metric names — pinned so dashboards and alert rules
/// (`infra/k8s/platform/manifests/observability/prometheus-rules-audit-sink.yaml`)
/// reference them as constants.
pub mod metrics {
    /// Histogram (seconds): gap between `event.at` and the moment
    /// the snapshot containing that record was committed in Iceberg.
    /// SLO P99 < 90s under steady load.
    pub const SINK_LAG_SECONDS: &str = "audit_sink_lag_seconds";

    /// Counter: records persisted to Iceberg.
    pub const SINK_RECORDS_TOTAL: &str = "audit_sink_records_total";

    /// Histogram: number of records per Iceberg snapshot.
    pub const SINK_BATCH_SIZE: &str = "audit_sink_batch_size_records";

    /// Counter: snapshot commits, labelled `outcome={ok,fail}`.
    pub const SINK_COMMITS_TOTAL: &str = "audit_sink_commits_total";
}
