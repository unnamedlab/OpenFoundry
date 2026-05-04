//! Kafka → Iceberg materialisation runtime for `lineage-service`.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};

use arrow_array::{FixedSizeBinaryArray, RecordBatch, StringArray, TimestampMicrosecondArray};
use arrow_schema::{ArrowError, DataType, Field, Schema, TimeUnit};
use chrono::{DateTime, Utc};
use event_bus_data::{
    CommitError, DataBusConfig, DataMessage, DataSubscriber, ServicePrincipal, SubscribeError,
};
use prometheus::{
    Encoder, HistogramVec, IntCounterVec, Opts, Registry, TextEncoder, histogram_opts,
};
use serde_json::Value;
use storage_abstraction::iceberg::{IcebergError, IcebergTable};
use thiserror::Error;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use uuid::Uuid;

use crate::{iceberg_schema, kafka_to_iceberg};

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
    #[error("invalid lineage event: {0}")]
    Decode(String),
    #[error("arrow batch build failed: {0}")]
    Arrow(#[from] ArrowError),
    #[error("iceberg write failed: {0}")]
    Iceberg(#[from] IcebergError),
    #[error("json serialization failed: {0}")]
    Json(#[from] serde_json::Error),
}

#[derive(Debug, Clone)]
pub struct RuntimeConfig {
    pub data_bus: DataBusConfig,
    pub catalog_url: String,
    pub warehouse: Option<String>,
    pub batch_policy: BatchPolicy,
}

pub mod metrics {
    pub const SINK_LAG_SECONDS: &str = "lineage_service_lag_seconds";
    pub const SINK_RECORDS_TOTAL: &str = "lineage_service_records_total";
    pub const SINK_BATCH_SIZE: &str = "lineage_service_batch_size_records";
    pub const SINK_COMMITS_TOTAL: &str = "lineage_service_commits_total";
}

#[derive(Clone)]
pub struct RuntimeMetrics {
    registry: Arc<Registry>,
    sink_lag_seconds: HistogramVec,
    sink_records_total: IntCounterVec,
    sink_batch_size: HistogramVec,
    sink_commits_total: IntCounterVec,
}

impl Default for RuntimeMetrics {
    fn default() -> Self {
        Self::new()
    }
}

impl RuntimeMetrics {
    pub fn new() -> Self {
        let registry = Arc::new(Registry::new());
        let sink_lag_seconds = HistogramVec::new(
            histogram_opts!(
                metrics::SINK_LAG_SECONDS,
                "Seconds between lineage event production time and successful Iceberg append.",
                vec![0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0]
            ),
            &["table"],
        )
        .expect("valid lineage_service_lag_seconds metric");
        let sink_records_total = IntCounterVec::new(
            Opts::new(
                metrics::SINK_RECORDS_TOTAL,
                "Total lineage rows appended to Iceberg.",
            ),
            &["table"],
        )
        .expect("valid lineage_service_records_total metric");
        let sink_batch_size = HistogramVec::new(
            histogram_opts!(
                metrics::SINK_BATCH_SIZE,
                "Lineage rows per successful Iceberg append.",
                vec![1.0, 10.0, 100.0, 1_000.0, 10_000.0, 25_000.0]
            ),
            &["table"],
        )
        .expect("valid lineage_service_batch_size_records metric");
        let sink_commits_total = IntCounterVec::new(
            Opts::new(
                metrics::SINK_COMMITS_TOTAL,
                "Lineage Iceberg append attempts by outcome.",
            ),
            &["table", "outcome"],
        )
        .expect("valid lineage_service_commits_total metric");

        registry
            .register(Box::new(sink_lag_seconds.clone()))
            .expect("register lineage_service_lag_seconds");
        registry
            .register(Box::new(sink_records_total.clone()))
            .expect("register lineage_service_records_total");
        registry
            .register(Box::new(sink_batch_size.clone()))
            .expect("register lineage_service_batch_size_records");
        registry
            .register(Box::new(sink_commits_total.clone()))
            .expect("register lineage_service_commits_total");

        Self {
            registry,
            sink_lag_seconds,
            sink_records_total,
            sink_batch_size,
            sink_commits_total,
        }
    }

    fn record_append_success(&self, table: &str, row_times_micros: impl IntoIterator<Item = i64>) {
        let now_micros = Utc::now().timestamp_micros();
        let times: Vec<i64> = row_times_micros.into_iter().collect();
        self.sink_records_total
            .with_label_values(&[table])
            .inc_by(times.len() as u64);
        self.sink_batch_size
            .with_label_values(&[table])
            .observe(times.len() as f64);
        self.sink_commits_total
            .with_label_values(&[table, "ok"])
            .inc();
        for event_micros in times {
            self.sink_lag_seconds
                .with_label_values(&[table])
                .observe(lag_seconds(event_micros, now_micros));
        }
    }

    fn record_append_failure(&self, table: &str) {
        self.sink_commits_total
            .with_label_values(&[table, "fail"])
            .inc();
    }

    pub fn render(&self) -> Result<String, prometheus::Error> {
        let mut buf = Vec::new();
        TextEncoder::new().encode(&self.registry.gather(), &mut buf)?;
        Ok(String::from_utf8(buf).unwrap_or_default())
    }
}

fn lag_seconds(event_micros: i64, now_micros: i64) -> f64 {
    (now_micros.saturating_sub(event_micros).max(0) as f64) / 1_000_000.0
}

#[derive(Debug, Clone, Copy)]
pub struct BatchPolicy {
    pub max_records: usize,
    pub max_wait: Duration,
}

impl BatchPolicy {
    pub const PLAN_DEFAULT: Self = Self {
        max_records: 25_000,
        max_wait: Duration::from_secs(30),
    };

    pub fn should_flush(self, records: usize, elapsed: Duration) -> bool {
        records >= self.max_records || elapsed >= self.max_wait
    }
}

pub struct TableSet {
    runs: IcebergTable,
    events: IcebergTable,
    datasets_io: IcebergTable,
}

#[derive(Debug)]
struct PendingRecord {
    event: MaterializedEvent,
    message: DataMessage,
}

#[derive(Debug, Clone)]
struct MaterializedEvent {
    run: RunRow,
    event: EventRow,
    datasets_io: Vec<DatasetIoRow>,
}

#[derive(Debug, Clone)]
struct RunRow {
    run_id: Uuid,
    job_namespace: String,
    job_name: String,
    started_at: i64,
    completed_at: Option<i64>,
    state: String,
    facets_json: String,
}

#[derive(Debug, Clone)]
struct EventRow {
    event_id: Uuid,
    run_id: Uuid,
    event_time: i64,
    event_type: String,
    producer: String,
    schema_url: Option<String>,
    payload_json: String,
}

#[derive(Debug, Clone)]
struct DatasetIoRow {
    run_id: Uuid,
    event_time: i64,
    side: &'static str,
    dataset_namespace: String,
    dataset_name: String,
    facets_json: String,
}

impl RuntimeConfig {
    pub fn from_env() -> Result<Self, RuntimeError> {
        let catalog_url = non_empty_env("ICEBERG_CATALOG_URL")
            .map_err(|_| RuntimeError::MissingEnv("ICEBERG_CATALOG_URL"))?;
        Ok(Self {
            data_bus: data_bus_config_from_env(kafka_to_iceberg::CONSUMER_GROUP)?,
            catalog_url,
            warehouse: non_empty_env("ICEBERG_WAREHOUSE").ok(),
            batch_policy: batch_policy_from_env()?,
        })
    }
}

impl TableSet {
    fn runs(&mut self) -> &mut IcebergTable {
        &mut self.runs
    }

    fn events(&mut self) -> &mut IcebergTable {
        &mut self.events
    }

    fn datasets_io(&mut self) -> &mut IcebergTable {
        &mut self.datasets_io
    }
}

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
    if let Ok(value) = non_empty_env("LINEAGE_BATCH_MAX_RECORDS") {
        policy.max_records = value
            .parse::<usize>()
            .map_err(|_| RuntimeError::InvalidEnv {
                key: "LINEAGE_BATCH_MAX_RECORDS",
                value: value.clone(),
                reason: "expected positive integer",
            })?;
    }
    if let Ok(value) = non_empty_env("LINEAGE_BATCH_MAX_WAIT_SECONDS") {
        let seconds = value.parse::<u64>().map_err(|_| RuntimeError::InvalidEnv {
            key: "LINEAGE_BATCH_MAX_WAIT_SECONDS",
            value: value.clone(),
            reason: "expected positive integer",
        })?;
        policy.max_wait = Duration::from_secs(seconds);
    }
    Ok(policy)
}

pub async fn load_tables(config: &RuntimeConfig) -> Result<TableSet, RuntimeError> {
    Ok(TableSet {
        runs: load_table(config, kafka_to_iceberg::iceberg_target::TABLE_RUNS).await?,
        events: load_table(config, kafka_to_iceberg::iceberg_target::TABLE_EVENTS).await?,
        datasets_io: load_table(config, kafka_to_iceberg::iceberg_target::TABLE_DATASETS_IO)
            .await?,
    })
}

async fn load_table(config: &RuntimeConfig, table: &str) -> Result<IcebergTable, RuntimeError> {
    let namespace = [kafka_to_iceberg::iceberg_target::NAMESPACE];
    match &config.warehouse {
        Some(warehouse) => Ok(IcebergTable::load_table_with_warehouse(
            &config.catalog_url,
            warehouse,
            &namespace,
            table,
        )
        .await?),
        None => Ok(IcebergTable::load_table(&config.catalog_url, &namespace, table).await?),
    }
}

pub async fn run<S>(
    subscriber: S,
    tables: TableSet,
    batch_policy: BatchPolicy,
) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    run_with_metrics(subscriber, tables, batch_policy, None).await
}

pub async fn run_with_metrics<S>(
    subscriber: S,
    mut tables: TableSet,
    batch_policy: BatchPolicy,
    metrics: Option<Arc<RuntimeMetrics>>,
) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    subscriber.subscribe(&[kafka_to_iceberg::SOURCE_TOPIC])?;
    tracing::info!(
        group = kafka_to_iceberg::CONSUMER_GROUP,
        topic = kafka_to_iceberg::SOURCE_TOPIC,
        max_records = batch_policy.max_records,
        max_wait_seconds = batch_policy.max_wait.as_secs(),
        "lineage-service consumer loop started"
    );

    let mut batch = Vec::with_capacity(batch_policy.max_records.min(2048));
    let mut first_record_at = Instant::now();

    loop {
        if !batch.is_empty() && batch_policy.should_flush(batch.len(), first_record_at.elapsed()) {
            flush_batch(&subscriber, &mut tables, &mut batch, metrics.as_deref()).await?;
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
                    flush_batch(&subscriber, &mut tables, &mut batch, metrics.as_deref()).await?;
                    first_record_at = Instant::now();
                    continue;
                }
            }
        };

        let Some(payload) = message.payload() else {
            tracing::warn!(
                partition = message.partition(),
                offset = message.offset(),
                "lineage-service skipping record without payload"
            );
            subscriber.commit(&message)?;
            continue;
        };

        match decode_event(payload) {
            Ok(event) => {
                if batch.is_empty() {
                    first_record_at = Instant::now();
                }
                batch.push(PendingRecord { event, message });
            }
            Err(error) => {
                tracing::warn!(
                    partition = message.partition(),
                    offset = message.offset(),
                    %error,
                    "lineage-service skipping malformed lineage event"
                );
                subscriber.commit(&message)?;
            }
        }
    }
}

async fn flush_batch<S>(
    subscriber: &S,
    tables: &mut TableSet,
    batch: &mut Vec<PendingRecord>,
    metrics: Option<&RuntimeMetrics>,
) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    if batch.is_empty() {
        return Ok(());
    }

    let started = Instant::now();
    let runs: Vec<RunRow> = batch
        .iter()
        .map(|record| record.event.run.clone())
        .collect();
    let events: Vec<EventRow> = batch
        .iter()
        .map(|record| record.event.event.clone())
        .collect();
    let datasets_io: Vec<DatasetIoRow> = batch
        .iter()
        .flat_map(|record| record.event.datasets_io.clone())
        .collect();

    if let Err(error) = tables
        .runs()
        .append_record_batches(vec![runs_to_record_batch(&runs)?])
        .await
    {
        if let Some(metrics) = metrics {
            metrics.record_append_failure(kafka_to_iceberg::iceberg_target::TABLE_RUNS);
        }
        return Err(error.into());
    }
    if let Some(metrics) = metrics {
        metrics.record_append_success(
            kafka_to_iceberg::iceberg_target::TABLE_RUNS,
            runs.iter().map(|row| row.started_at),
        );
    }

    if let Err(error) = tables
        .events()
        .append_record_batches(vec![events_to_record_batch(&events)?])
        .await
    {
        if let Some(metrics) = metrics {
            metrics.record_append_failure(kafka_to_iceberg::iceberg_target::TABLE_EVENTS);
        }
        return Err(error.into());
    }
    if let Some(metrics) = metrics {
        metrics.record_append_success(
            kafka_to_iceberg::iceberg_target::TABLE_EVENTS,
            events.iter().map(|row| row.event_time),
        );
    }

    if !datasets_io.is_empty() {
        if let Err(error) = tables
            .datasets_io()
            .append_record_batches(vec![datasets_to_record_batch(&datasets_io)?])
            .await
        {
            if let Some(metrics) = metrics {
                metrics.record_append_failure(kafka_to_iceberg::iceberg_target::TABLE_DATASETS_IO);
            }
            return Err(error.into());
        }
        if let Some(metrics) = metrics {
            metrics.record_append_success(
                kafka_to_iceberg::iceberg_target::TABLE_DATASETS_IO,
                datasets_io.iter().map(|row| row.event_time),
            );
        }
    }

    for record in batch.iter() {
        subscriber.commit(&record.message)?;
    }

    tracing::info!(
        rows = batch.len(),
        dataset_edges = datasets_io.len(),
        elapsed_ms = started.elapsed().as_millis(),
        namespace = kafka_to_iceberg::iceberg_target::NAMESPACE,
        "lineage events committed to Iceberg"
    );

    batch.clear();
    Ok(())
}

pub fn metrics_addr_from_env(default_port: u16) -> Result<SocketAddr, RuntimeError> {
    let value = std::env::var("METRICS_ADDR").unwrap_or_else(|_| format!("0.0.0.0:{default_port}"));
    value
        .parse::<SocketAddr>()
        .map_err(|_| RuntimeError::InvalidEnv {
            key: "METRICS_ADDR",
            value,
            reason: "expected socket address, for example 0.0.0.0:9090",
        })
}

pub async fn serve_metrics(metrics: Arc<RuntimeMetrics>, addr: SocketAddr) -> std::io::Result<()> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    loop {
        let (mut stream, _) = listener.accept().await?;
        let metrics = Arc::clone(&metrics);
        tokio::spawn(async move {
            let mut buf = [0_u8; 1024];
            let read =
                tokio::time::timeout(std::time::Duration::from_secs(2), stream.read(&mut buf))
                    .await
                    .ok()
                    .and_then(Result::ok)
                    .unwrap_or(0);
            let request = String::from_utf8_lossy(&buf[..read]);
            let path = request.split_whitespace().nth(1).unwrap_or("/");
            let (status, content_type, body) = match path {
                "/health" | "/healthz" => ("200 OK", "text/plain; charset=utf-8", "ok\n".into()),
                "/metrics" => match metrics.render() {
                    Ok(body) => ("200 OK", "text/plain; version=0.0.4", body),
                    Err(error) => (
                        "500 Internal Server Error",
                        "text/plain; charset=utf-8",
                        format!("failed to render metrics: {error}\n"),
                    ),
                },
                _ => (
                    "404 Not Found",
                    "text/plain; charset=utf-8",
                    "not found\n".into(),
                ),
            };
            let response = format!(
                "HTTP/1.1 {status}\r\ncontent-type: {content_type}\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = stream.write_all(response.as_bytes()).await;
        });
    }
}

fn decode_event(bytes: &[u8]) -> Result<MaterializedEvent, RuntimeError> {
    let payload: Value = serde_json::from_slice(bytes)?;
    let event_type = required_string(&payload, &["eventType", "event_type"])?;
    let event_time = parse_event_time(required_value(&payload, &["eventTime", "event_time"])?)?;
    let producer = optional_string(&payload, &["producer"]).unwrap_or_else(|| "unknown".into());
    let schema_url = optional_string(&payload, &["schemaURL", "schema_url"]);

    let run = required_object(&payload, "run")?;
    let raw_run_id = required_string_from_map(run, &["runId", "run_id"])?;
    let run_id = parse_uuid_or_v5(&raw_run_id);

    let job = required_object(&payload, "job")?;
    let job_namespace = required_string_from_map(job, &["namespace"])?;
    let job_name = required_string_from_map(job, &["name"])?;
    let facets_json = serde_json::to_string(
        run.get("facets")
            .unwrap_or(&Value::Object(Default::default())),
    )?;
    let payload_json = serde_json::to_string(&payload)?;

    let event_id = derive_event_id(&run_id, &event_type, event_time, &job_namespace, &job_name);
    let normalized_state = normalize_event_type(&event_type);
    let completed_at = if is_terminal_event(&normalized_state) {
        Some(event_time)
    } else {
        None
    };

    let mut datasets_io = Vec::new();
    if let Some(inputs) = payload.get("inputs").and_then(Value::as_array) {
        for dataset in inputs {
            datasets_io.push(dataset_row(
                run_id,
                event_time,
                dataset,
                iceberg_schema::datasets_io::SIDE_INPUT,
            )?);
        }
    }
    if let Some(outputs) = payload.get("outputs").and_then(Value::as_array) {
        for dataset in outputs {
            datasets_io.push(dataset_row(
                run_id,
                event_time,
                dataset,
                iceberg_schema::datasets_io::SIDE_OUTPUT,
            )?);
        }
    }

    Ok(MaterializedEvent {
        run: RunRow {
            run_id,
            job_namespace,
            job_name,
            started_at: event_time,
            completed_at,
            state: normalized_state.clone(),
            facets_json,
        },
        event: EventRow {
            event_id,
            run_id,
            event_time,
            event_type: normalized_state,
            producer,
            schema_url,
            payload_json,
        },
        datasets_io,
    })
}

fn dataset_row(
    run_id: Uuid,
    event_time: i64,
    dataset: &Value,
    side: &'static str,
) -> Result<DatasetIoRow, RuntimeError> {
    let object = dataset
        .as_object()
        .ok_or_else(|| RuntimeError::Decode("dataset entry is not an object".into()))?;
    let dataset_namespace = required_string_from_map(object, &["namespace"])?;
    let dataset_name = required_string_from_map(object, &["name"])?;
    let facets_json = serde_json::to_string(
        object
            .get("facets")
            .unwrap_or(&Value::Object(Default::default())),
    )?;
    Ok(DatasetIoRow {
        run_id,
        event_time,
        side,
        dataset_namespace,
        dataset_name,
        facets_json,
    })
}

fn required_value<'a>(payload: &'a Value, keys: &[&str]) -> Result<&'a Value, RuntimeError> {
    keys.iter()
        .find_map(|key| payload.get(*key))
        .ok_or_else(|| RuntimeError::Decode(format!("missing required field {}", keys[0])))
}

fn required_string(payload: &Value, keys: &[&str]) -> Result<String, RuntimeError> {
    required_value(payload, keys)?
        .as_str()
        .map(ToString::to_string)
        .ok_or_else(|| RuntimeError::Decode(format!("field {} must be a string", keys[0])))
}

fn optional_string(payload: &Value, keys: &[&str]) -> Option<String> {
    keys.iter()
        .find_map(|key| payload.get(*key))
        .and_then(Value::as_str)
        .map(ToString::to_string)
}

fn required_object<'a>(
    payload: &'a Value,
    key: &str,
) -> Result<&'a serde_json::Map<String, Value>, RuntimeError> {
    payload
        .get(key)
        .and_then(Value::as_object)
        .ok_or_else(|| RuntimeError::Decode(format!("missing required object {key}")))
}

fn required_string_from_map(
    payload: &serde_json::Map<String, Value>,
    keys: &[&str],
) -> Result<String, RuntimeError> {
    keys.iter()
        .find_map(|key| payload.get(*key))
        .and_then(Value::as_str)
        .map(ToString::to_string)
        .ok_or_else(|| RuntimeError::Decode(format!("missing required field {}", keys[0])))
}

fn parse_event_time(value: &Value) -> Result<i64, RuntimeError> {
    if let Some(text) = value.as_str() {
        let dt = DateTime::parse_from_rfc3339(text).map_err(|error| {
            RuntimeError::Decode(format!("invalid RFC3339 event time: {error}"))
        })?;
        return Ok(dt.with_timezone(&Utc).timestamp_micros());
    }
    if let Some(number) = value.as_i64() {
        return Ok(if number >= 1_000_000_000_000_000 {
            number
        } else if number >= 1_000_000_000_000 {
            number * 1_000
        } else {
            number * 1_000_000
        });
    }
    Err(RuntimeError::Decode(
        "eventTime must be RFC3339 string or epoch integer".into(),
    ))
}

fn parse_uuid_or_v5(raw: &str) -> Uuid {
    Uuid::parse_str(raw).unwrap_or_else(|_| Uuid::new_v5(&Uuid::NAMESPACE_URL, raw.as_bytes()))
}

fn derive_event_id(
    run_id: &Uuid,
    event_type: &str,
    event_time: i64,
    job_namespace: &str,
    job_name: &str,
) -> Uuid {
    let key = format!("{run_id}|{event_type}|{event_time}|{job_namespace}|{job_name}");
    Uuid::new_v5(&Uuid::NAMESPACE_OID, key.as_bytes())
}

fn normalize_event_type(event_type: &str) -> String {
    event_type.trim().to_ascii_uppercase()
}

fn is_terminal_event(event_type: &str) -> bool {
    matches!(event_type, "COMPLETE" | "FAIL" | "ABORT")
}

fn runs_to_record_batch(rows: &[RunRow]) -> Result<RecordBatch, RuntimeError> {
    let run_ids: Vec<[u8; 16]> = rows.iter().map(|row| *row.run_id.as_bytes()).collect();
    let job_namespaces: Vec<&str> = rows.iter().map(|row| row.job_namespace.as_str()).collect();
    let job_names: Vec<&str> = rows.iter().map(|row| row.job_name.as_str()).collect();
    let started_at: Vec<i64> = rows.iter().map(|row| row.started_at).collect();
    let states: Vec<&str> = rows.iter().map(|row| row.state.as_str()).collect();
    let facets: Vec<&str> = rows.iter().map(|row| row.facets_json.as_str()).collect();

    let mut completed_at = TimestampMicrosecondArray::builder(rows.len());
    for row in rows {
        match row.completed_at {
            Some(value) => completed_at.append_value(value),
            None => completed_at.append_null(),
        }
    }

    Ok(RecordBatch::try_new(
        Arc::new(runs_arrow_schema()),
        vec![
            Arc::new(FixedSizeBinaryArray::try_from_iter(run_ids.into_iter())?),
            Arc::new(StringArray::from(job_namespaces)),
            Arc::new(StringArray::from(job_names)),
            Arc::new(TimestampMicrosecondArray::from(started_at).with_timezone_utc()),
            Arc::new(completed_at.finish().with_timezone_utc()),
            Arc::new(StringArray::from(states)),
            Arc::new(StringArray::from(facets)),
        ],
    )?)
}

fn events_to_record_batch(rows: &[EventRow]) -> Result<RecordBatch, RuntimeError> {
    let event_ids: Vec<[u8; 16]> = rows.iter().map(|row| *row.event_id.as_bytes()).collect();
    let run_ids: Vec<[u8; 16]> = rows.iter().map(|row| *row.run_id.as_bytes()).collect();
    let event_time: Vec<i64> = rows.iter().map(|row| row.event_time).collect();
    let event_types: Vec<&str> = rows.iter().map(|row| row.event_type.as_str()).collect();
    let producers: Vec<&str> = rows.iter().map(|row| row.producer.as_str()).collect();
    let payloads: Vec<&str> = rows.iter().map(|row| row.payload_json.as_str()).collect();
    let schema_urls: Vec<Option<String>> = rows.iter().map(|row| row.schema_url.clone()).collect();

    Ok(RecordBatch::try_new(
        Arc::new(events_arrow_schema()),
        vec![
            Arc::new(FixedSizeBinaryArray::try_from_iter(event_ids.into_iter())?),
            Arc::new(FixedSizeBinaryArray::try_from_iter(run_ids.into_iter())?),
            Arc::new(TimestampMicrosecondArray::from(event_time).with_timezone_utc()),
            Arc::new(StringArray::from(event_types)),
            Arc::new(StringArray::from(producers)),
            Arc::new(StringArray::from(schema_urls)),
            Arc::new(StringArray::from(payloads)),
        ],
    )?)
}

fn datasets_to_record_batch(rows: &[DatasetIoRow]) -> Result<RecordBatch, RuntimeError> {
    let run_ids: Vec<[u8; 16]> = rows.iter().map(|row| *row.run_id.as_bytes()).collect();
    let event_time: Vec<i64> = rows.iter().map(|row| row.event_time).collect();
    let sides: Vec<&str> = rows.iter().map(|row| row.side).collect();
    let namespaces: Vec<&str> = rows
        .iter()
        .map(|row| row.dataset_namespace.as_str())
        .collect();
    let names: Vec<&str> = rows.iter().map(|row| row.dataset_name.as_str()).collect();
    let facets: Vec<&str> = rows.iter().map(|row| row.facets_json.as_str()).collect();

    Ok(RecordBatch::try_new(
        Arc::new(datasets_arrow_schema()),
        vec![
            Arc::new(FixedSizeBinaryArray::try_from_iter(run_ids.into_iter())?),
            Arc::new(TimestampMicrosecondArray::from(event_time).with_timezone_utc()),
            Arc::new(StringArray::from(sides)),
            Arc::new(StringArray::from(namespaces)),
            Arc::new(StringArray::from(names)),
            Arc::new(StringArray::from(facets)),
        ],
    )?)
}

fn runs_arrow_schema() -> Schema {
    Schema::new(vec![
        arrow_field(
            iceberg_schema::runs::fields::RUN_ID,
            DataType::FixedSizeBinary(16),
            false,
            iceberg_schema::runs::field_ids::RUN_ID,
        ),
        arrow_field(
            iceberg_schema::runs::fields::JOB_NAMESPACE,
            DataType::Utf8,
            false,
            iceberg_schema::runs::field_ids::JOB_NAMESPACE,
        ),
        arrow_field(
            iceberg_schema::runs::fields::JOB_NAME,
            DataType::Utf8,
            false,
            iceberg_schema::runs::field_ids::JOB_NAME,
        ),
        arrow_field(
            iceberg_schema::runs::fields::STARTED_AT,
            DataType::Timestamp(TimeUnit::Microsecond, Some("+00:00".into())),
            false,
            iceberg_schema::runs::field_ids::STARTED_AT,
        ),
        arrow_field(
            iceberg_schema::runs::fields::COMPLETED_AT,
            DataType::Timestamp(TimeUnit::Microsecond, Some("+00:00".into())),
            true,
            iceberg_schema::runs::field_ids::COMPLETED_AT,
        ),
        arrow_field(
            iceberg_schema::runs::fields::STATE,
            DataType::Utf8,
            false,
            iceberg_schema::runs::field_ids::STATE,
        ),
        arrow_field(
            iceberg_schema::runs::fields::FACETS,
            DataType::Utf8,
            false,
            iceberg_schema::runs::field_ids::FACETS,
        ),
    ])
}

fn events_arrow_schema() -> Schema {
    Schema::new(vec![
        arrow_field(
            iceberg_schema::events::fields::EVENT_ID,
            DataType::FixedSizeBinary(16),
            false,
            iceberg_schema::events::field_ids::EVENT_ID,
        ),
        arrow_field(
            iceberg_schema::events::fields::RUN_ID,
            DataType::FixedSizeBinary(16),
            false,
            iceberg_schema::events::field_ids::RUN_ID,
        ),
        arrow_field(
            iceberg_schema::events::fields::EVENT_TIME,
            DataType::Timestamp(TimeUnit::Microsecond, Some("+00:00".into())),
            false,
            iceberg_schema::events::field_ids::EVENT_TIME,
        ),
        arrow_field(
            iceberg_schema::events::fields::EVENT_TYPE,
            DataType::Utf8,
            false,
            iceberg_schema::events::field_ids::EVENT_TYPE,
        ),
        arrow_field(
            iceberg_schema::events::fields::PRODUCER,
            DataType::Utf8,
            false,
            iceberg_schema::events::field_ids::PRODUCER,
        ),
        arrow_field(
            iceberg_schema::events::fields::SCHEMA_URL,
            DataType::Utf8,
            true,
            iceberg_schema::events::field_ids::SCHEMA_URL,
        ),
        arrow_field(
            iceberg_schema::events::fields::PAYLOAD,
            DataType::Utf8,
            false,
            iceberg_schema::events::field_ids::PAYLOAD,
        ),
    ])
}

fn datasets_arrow_schema() -> Schema {
    Schema::new(vec![
        arrow_field(
            iceberg_schema::datasets_io::fields::RUN_ID,
            DataType::FixedSizeBinary(16),
            false,
            iceberg_schema::datasets_io::field_ids::RUN_ID,
        ),
        arrow_field(
            iceberg_schema::datasets_io::fields::EVENT_TIME,
            DataType::Timestamp(TimeUnit::Microsecond, Some("+00:00".into())),
            false,
            iceberg_schema::datasets_io::field_ids::EVENT_TIME,
        ),
        arrow_field(
            iceberg_schema::datasets_io::fields::SIDE,
            DataType::Utf8,
            false,
            iceberg_schema::datasets_io::field_ids::SIDE,
        ),
        arrow_field(
            iceberg_schema::datasets_io::fields::DATASET_NAMESPACE,
            DataType::Utf8,
            false,
            iceberg_schema::datasets_io::field_ids::DATASET_NAMESPACE,
        ),
        arrow_field(
            iceberg_schema::datasets_io::fields::DATASET_NAME,
            DataType::Utf8,
            false,
            iceberg_schema::datasets_io::field_ids::DATASET_NAME,
        ),
        arrow_field(
            iceberg_schema::datasets_io::fields::FACETS,
            DataType::Utf8,
            false,
            iceberg_schema::datasets_io::field_ids::FACETS,
        ),
    ])
}

fn arrow_field(name: &'static str, data_type: DataType, nullable: bool, field_id: i32) -> Field {
    Field::new(name, data_type, nullable).with_metadata(HashMap::from([(
        "PARQUET:field_id".to_string(),
        field_id.to_string(),
    )]))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn sample_event() -> Value {
        json!({
            "eventType": "COMPLETE",
            "eventTime": "2026-05-03T10:11:12Z",
            "producer": "urn:openfoundry:test",
            "schemaURL": "https://openlineage.io/spec/1-0-5/OpenLineage.json",
            "run": {
                "runId": "f1b9c3e0-2a6f-4d7a-8ad3-95f73b4a3d52",
                "facets": { "nominalTime": { "nominalStartTime": "2026-05-03T10:00:00Z" } }
            },
            "job": {
                "namespace": "of://pipelines",
                "name": "pipeline.build"
            },
            "inputs": [
                { "namespace": "of://datasets", "name": "source-a", "facets": { "schema": {} } }
            ],
            "outputs": [
                { "namespace": "of://datasets", "name": "target-b", "facets": { "schema": {} } }
            ]
        })
    }

    #[test]
    fn decodes_openlineage_payload_into_three_materializations() {
        let event = decode_event(&serde_json::to_vec(&sample_event()).unwrap()).unwrap();
        assert_eq!(event.run.job_name, "pipeline.build");
        assert_eq!(event.event.event_type, "COMPLETE");
        assert_eq!(event.datasets_io.len(), 2);
        assert!(event.run.completed_at.is_some());
    }

    #[test]
    fn run_id_falls_back_to_uuid_v5_when_not_uuid() {
        let mut payload = sample_event();
        payload["run"]["runId"] = json!("external-run-id");
        let event = decode_event(&serde_json::to_vec(&payload).unwrap()).unwrap();
        assert_ne!(event.run.run_id, Uuid::nil());
    }

    #[test]
    fn arrow_batches_preserve_row_counts() {
        let event = decode_event(&serde_json::to_vec(&sample_event()).unwrap()).unwrap();
        let run_batch = runs_to_record_batch(std::slice::from_ref(&event.run)).unwrap();
        let event_batch = events_to_record_batch(std::slice::from_ref(&event.event)).unwrap();
        let edge_batch = datasets_to_record_batch(&event.datasets_io).unwrap();
        assert_eq!(run_batch.num_rows(), 1);
        assert_eq!(event_batch.num_rows(), 1);
        assert_eq!(edge_batch.num_rows(), 2);
    }

    #[test]
    fn runtime_metrics_render_prometheus_text() {
        let event = decode_event(&serde_json::to_vec(&sample_event()).unwrap()).unwrap();
        let metrics = RuntimeMetrics::new();
        metrics.record_append_success(
            kafka_to_iceberg::iceberg_target::TABLE_EVENTS,
            [event.event.event_time],
        );
        let body = metrics.render().unwrap();
        assert!(body.contains(metrics::SINK_RECORDS_TOTAL));
        assert!(body.contains("table=\"events\""));
        assert!(body.contains(metrics::SINK_LAG_SECONDS));
    }

    #[test]
    fn runtime_metrics_track_failed_append_attempts() {
        let metrics = RuntimeMetrics::new();
        metrics.record_append_failure(kafka_to_iceberg::iceberg_target::TABLE_RUNS);
        let body = metrics.render().unwrap();
        assert!(body.contains(metrics::SINK_COMMITS_TOTAL));
        assert!(body.contains("outcome=\"fail\""));
        assert!(body.contains("table=\"runs\""));
    }
}
