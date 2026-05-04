//! Runtime wiring (gated by `runtime`).
//!
//! Glues the pure logic in [`crate::event`] / [`crate::scan`] /
//! [`crate::state`] to the live Kafka consumer + producer
//! (`event-bus-data`), the Postgres state repo (`sqlx`), and the
//! Cassandra paginator (`cassandra-kernel`). Also exposes the
//! Prometheus metrics and the small HTTP control plane the
//! `:9090/metrics` and `:8080/health` endpoints scrape.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use event_bus_data::{
    DataBusConfig, DataMessage, DataPublisher, DataSubscriber, KafkaPublisher, OpenLineageHeaders,
    PublishError, ServicePrincipal,
};
use idempotency::{IdempotencyStore, Outcome, postgres::PgIdempotencyStore};
use prometheus::{Encoder, IntCounterVec, IntGauge, Opts, Registry, TextEncoder};
use thiserror::Error;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::time::sleep;
use uuid::Uuid;

use crate::event::{ReindexCompletedV1, derive_batch_event_id, derive_job_id};
use crate::scan::{
    CassandraScanner, DecodeError, DecodedRequest, ReindexRecord, ScanError, decode_request,
};
use crate::state::{JobRecord, JobRepo, JobStatus, StateError};
use crate::topics::{
    ONTOLOGY_REINDEX_COMPLETED_V1, ONTOLOGY_REINDEX_REQUESTED_V1, ONTOLOGY_REINDEX_V1,
};

/// Kafka consumer group used by every replica of the coordinator.
/// Pinned here so a typo across replicas does not silently fork
/// the rebalance state (same convention as `ontology-indexer`).
pub const CONSUMER_GROUP: &str = "reindex-coordinator-service";

/// Topics the coordinator subscribes to. Pinned for the same
/// reason as [`CONSUMER_GROUP`].
pub const SUBSCRIBE_TOPICS: &[&str] = &[ONTOLOGY_REINDEX_REQUESTED_V1];

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
    #[error("kafka subscribe failed: {0}")]
    Subscribe(#[from] event_bus_data::SubscribeError),
    #[error("kafka commit failed: {0}")]
    Commit(#[from] event_bus_data::CommitError),
    #[error("kafka publish failed: {0}")]
    Publish(#[from] PublishError),
    #[error("postgres / state error: {0}")]
    State(#[from] StateError),
    #[error("cassandra scan error: {0}")]
    Scan(#[from] ScanError),
    #[error("idempotency error: {0}")]
    Idempotency(#[from] idempotency::IdempotencyError),
    #[error("malformed request payload: {0}")]
    Decode(#[from] DecodeError),
}

// ─────────────────── Throttling configuration ───────────────────

/// Rate-limit knobs. The legacy Go worker leaned on Temporal's
/// retry backoff (1s → 60s exp) as an implicit rate-limiter; the
/// Rust port makes it explicit so a full backfill cannot saturate
/// `objects_by_id`. All knobs read from env at startup, with
/// defaults that match the old worker's default page size and
/// effective inter-page interval.
#[derive(Debug, Clone, Copy)]
pub struct Throttle {
    /// Sleep between successive Cassandra page fetches for the
    /// same job. `0` ⇒ no sleep. Source: `OF_REINDEX_PAGE_INTERVAL_MS`.
    pub page_interval: Duration,
    /// Hard cap on the total batches a single coordinator process
    /// publishes per second across all jobs (token-bucket style;
    /// implementation here is a simple per-publish sleep). Source:
    /// `OF_REINDEX_MAX_BATCHES_PER_SECOND`. `0` ⇒ unbounded.
    pub max_batches_per_second: u32,
}

impl Throttle {
    pub fn from_env() -> Result<Self, RuntimeError> {
        let page_interval_ms = parse_u64_env("OF_REINDEX_PAGE_INTERVAL_MS", 0)?;
        let max_per_second = parse_u32_env("OF_REINDEX_MAX_BATCHES_PER_SECOND", 0)?;
        Ok(Self {
            page_interval: Duration::from_millis(page_interval_ms),
            max_batches_per_second: max_per_second,
        })
    }

    fn per_publish_sleep(&self) -> Duration {
        if self.max_batches_per_second == 0 {
            Duration::ZERO
        } else {
            Duration::from_millis(1_000 / u64::from(self.max_batches_per_second).max(1))
        }
    }
}

// ──────────────────────────── Env helpers ────────────────────────────

fn parse_u64_env(key: &'static str, default: u64) -> Result<u64, RuntimeError> {
    match std::env::var(key) {
        Err(_) => Ok(default),
        Ok(s) => s
            .trim()
            .parse::<u64>()
            .map_err(|_| RuntimeError::InvalidEnv {
                key,
                value: s,
                reason: "expected unsigned integer",
            }),
    }
}

fn parse_u32_env(key: &'static str, default: u32) -> Result<u32, RuntimeError> {
    match std::env::var(key) {
        Err(_) => Ok(default),
        Ok(s) => s
            .trim()
            .parse::<u32>()
            .map_err(|_| RuntimeError::InvalidEnv {
                key,
                value: s,
                reason: "expected unsigned integer",
            }),
    }
}

fn non_empty_env(key: &'static str) -> Option<String> {
    std::env::var(key).ok().and_then(|value| {
        let trimmed = value.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed.to_string())
        }
    })
}

/// Build the `event-bus-data` config from the standard OpenFoundry
/// Kafka env vars. Same shape as `ontology-indexer::runtime`.
pub fn data_bus_config_from_env(service_name: &str) -> Result<DataBusConfig, RuntimeError> {
    let brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS")
        .map_err(|_| RuntimeError::MissingEnv("KAFKA_BOOTSTRAP_SERVERS"))?;
    let service = non_empty_env("KAFKA_SASL_USERNAME")
        .or_else(|| non_empty_env("KAFKA_CLIENT_ID"))
        .unwrap_or_else(|| service_name.to_string());

    let mut principal = match non_empty_env("KAFKA_SASL_PASSWORD") {
        Some(password) => ServicePrincipal::scram_sha_512(service, password),
        None => ServicePrincipal::insecure_dev(service),
    };
    if let Some(mechanism) = non_empty_env("KAFKA_SASL_MECHANISM") {
        principal.mechanism = mechanism;
    }
    if let Some(protocol) = non_empty_env("KAFKA_SECURITY_PROTOCOL") {
        principal.security_protocol = protocol;
    }
    Ok(DataBusConfig::new(brokers, principal))
}

// ───────────────────────────── Metrics ─────────────────────────────

#[derive(Clone)]
pub struct RuntimeMetrics {
    registry: Arc<Registry>,
    requests_total: IntCounterVec,
    batches_total: IntCounterVec,
    records_total: IntCounterVec,
    jobs_in_flight: IntGauge,
}

impl Default for RuntimeMetrics {
    fn default() -> Self {
        Self::new()
    }
}

impl RuntimeMetrics {
    pub fn new() -> Self {
        let registry = Arc::new(Registry::new());
        let requests_total = IntCounterVec::new(
            Opts::new(
                "reindex_coordinator_requests_total",
                "Reindex requests received and classified.",
            ),
            &["outcome"],
        )
        .expect("valid reindex_coordinator_requests_total metric");
        let batches_total = IntCounterVec::new(
            Opts::new(
                "reindex_coordinator_batches_total",
                "Batches produced to ontology.reindex.v1, by outcome.",
            ),
            &["outcome"],
        )
        .expect("valid reindex_coordinator_batches_total metric");
        let records_total = IntCounterVec::new(
            Opts::new(
                "reindex_coordinator_records_total",
                "Records contained in published batches, by outcome.",
            ),
            &["outcome"],
        )
        .expect("valid reindex_coordinator_records_total metric");
        let jobs_in_flight = IntGauge::new(
            "reindex_coordinator_jobs_in_flight",
            "Number of jobs currently in the running state.",
        )
        .expect("valid reindex_coordinator_jobs_in_flight metric");

        registry
            .register(Box::new(requests_total.clone()))
            .expect("register requests_total");
        registry
            .register(Box::new(batches_total.clone()))
            .expect("register batches_total");
        registry
            .register(Box::new(records_total.clone()))
            .expect("register records_total");
        registry
            .register(Box::new(jobs_in_flight.clone()))
            .expect("register jobs_in_flight");

        Self {
            registry,
            requests_total,
            batches_total,
            records_total,
            jobs_in_flight,
        }
    }

    pub fn render(&self) -> Result<String, prometheus::Error> {
        let mut buf = Vec::new();
        TextEncoder::new().encode(&self.registry.gather(), &mut buf)?;
        Ok(String::from_utf8(buf).unwrap_or_default())
    }
}

// ────────────────────────── Coordinator ──────────────────────────

/// All long-lived state shared between the consumer loop and the
/// per-job task.
pub struct Coordinator {
    pub jobs: Arc<JobRepo>,
    pub idempotency: Arc<PgIdempotencyStore>,
    pub scanner: Arc<CassandraScanner>,
    pub publisher: Arc<dyn DataPublisher>,
    pub metrics: Arc<RuntimeMetrics>,
    pub throttle: Throttle,
    pub lineage_namespace: String,
}

impl Coordinator {
    /// Run one in-flight job to a terminal status. Idempotent on
    /// every page boundary: a redelivery of the same Kafka message
    /// re-discovers the row, picks up at the persisted
    /// `resume_token`, and the per-batch idempotency store skips
    /// already-published pages.
    pub async fn run_job(
        &self,
        job_id: Uuid,
        request_id: Option<String>,
    ) -> Result<JobRecord, RuntimeError> {
        // Move queued → running. If the row is already terminal
        // (e.g. a duplicate `requested.v1` for a finished job),
        // surface that to the caller without resurrecting it.
        match self.jobs.mark_running(job_id).await {
            Ok(()) => {}
            Err(StateError::IllegalTransition { from, .. }) if from.is_terminal() => {
                tracing::info!(
                    %job_id,
                    status = %from,
                    "skipping run for terminal job (duplicate requested.v1)"
                );
                return self.jobs.load(job_id).await.map_err(RuntimeError::from);
            }
            Err(e) => return Err(e.into()),
        }
        self.metrics.jobs_in_flight.inc();
        let result = self.run_job_inner(job_id, request_id.clone()).await;
        self.metrics.jobs_in_flight.dec();
        result
    }

    async fn run_job_inner(
        &self,
        job_id: Uuid,
        request_id: Option<String>,
    ) -> Result<JobRecord, RuntimeError> {
        let mut current = self.jobs.load(job_id).await?;
        let per_publish_sleep = self.throttle.per_publish_sleep();

        loop {
            let token = current.resume_token.clone();
            let batch_event_id = derive_batch_event_id(
                &current.tenant_id,
                current.type_id.as_deref(),
                token.as_deref().unwrap_or(""),
            );

            let outcome = self.idempotency.check_and_record(batch_event_id).await?;
            let page = self
                .scanner
                .scan_page(
                    &current.tenant_id,
                    current.type_id.as_deref(),
                    current.page_size,
                    token.as_deref(),
                )
                .await;

            let page = match page {
                Ok(p) => p,
                Err(e) => {
                    let msg = e.to_string();
                    let _ = self
                        .jobs
                        .mark_terminal(job_id, JobStatus::Failed, Some(&msg))
                        .await?;
                    let final_row = self.jobs.load(job_id).await?;
                    self.publish_completed(&final_row, request_id.clone())
                        .await?;
                    return Err(e.into());
                }
            };

            let batch_size = page.records.len();

            // First-seen → publish; redelivery → skip publish but
            // still advance state so we make forward progress.
            if outcome == Outcome::FirstSeen {
                if let Err(e) = self.publish_batch(&page.records, &current).await {
                    self.metrics
                        .batches_total
                        .with_label_values(&["publish_error"])
                        .inc();
                    let msg = e.to_string();
                    let _ = self
                        .jobs
                        .mark_terminal(job_id, JobStatus::Failed, Some(&msg))
                        .await?;
                    let final_row = self.jobs.load(job_id).await?;
                    self.publish_completed(&final_row, request_id.clone())
                        .await?;
                    return Err(e);
                }
                self.metrics
                    .batches_total
                    .with_label_values(&["published"])
                    .inc();
                self.metrics
                    .records_total
                    .with_label_values(&["published"])
                    .inc_by(batch_size as u64);
            } else {
                self.metrics
                    .batches_total
                    .with_label_values(&["deduped"])
                    .inc();
                self.metrics
                    .records_total
                    .with_label_values(&["deduped"])
                    .inc_by(batch_size as u64);
                tracing::info!(
                    %job_id,
                    %batch_event_id,
                    "batch already processed; skipping publish (idempotency replay)"
                );
            }

            // Persist row AFTER the publish (or skip). Order is
            // critical: if we crash here, the next attempt
            // re-derives the same `batch_event_id`, sees it in
            // `processed_events`, and correctly skips re-publishing.
            self.jobs
                .advance(
                    job_id,
                    page.next_token.as_deref(),
                    page.scanned as i64,
                    if outcome == Outcome::FirstSeen {
                        batch_size as i64
                    } else {
                        0
                    },
                )
                .await?;

            current = self.jobs.load(job_id).await?;

            if page.next_token.is_none() {
                let final_row = self
                    .jobs
                    .mark_terminal(job_id, JobStatus::Completed, None)
                    .await?;
                self.publish_completed(&final_row, request_id.clone())
                    .await?;
                return Ok(final_row);
            }

            if !per_publish_sleep.is_zero() {
                sleep(per_publish_sleep).await;
            }
            if !self.throttle.page_interval.is_zero() {
                sleep(self.throttle.page_interval).await;
            }
        }
    }

    async fn publish_batch(
        &self,
        records: &[ReindexRecord],
        job: &JobRecord,
    ) -> Result<(), RuntimeError> {
        for record in records {
            let payload = serde_json::to_vec(record).map_err(|e| {
                RuntimeError::Publish(PublishError::InvalidRecord(format!(
                    "encode reindex record: {e}"
                )))
            })?;
            let key = record.partition_key();
            let lineage = OpenLineageHeaders::new(
                self.lineage_namespace.clone(),
                format!(
                    "reindex/{}/{}",
                    job.tenant_id,
                    job.type_id.as_deref().unwrap_or("*")
                ),
                job.id.to_string(),
                CONSUMER_GROUP,
            );
            self.publisher
                .publish(
                    ONTOLOGY_REINDEX_V1,
                    Some(key.as_bytes()),
                    &payload,
                    &lineage,
                )
                .await?;
        }
        Ok(())
    }

    async fn publish_completed(
        &self,
        job: &JobRecord,
        request_id: Option<String>,
    ) -> Result<(), RuntimeError> {
        let event = ReindexCompletedV1 {
            job_id: job.id,
            tenant_id: job.tenant_id.clone(),
            type_id: job.type_id.clone(),
            scanned: job.scanned,
            published: job.published,
            status: job.status.as_str().to_string(),
            error: job.error.clone(),
            request_id,
        };
        let payload = serde_json::to_vec(&event).map_err(|e| {
            RuntimeError::Publish(PublishError::InvalidRecord(format!(
                "encode completed event: {e}"
            )))
        })?;
        let lineage = OpenLineageHeaders::new(
            self.lineage_namespace.clone(),
            format!("reindex/{}", job.tenant_id),
            job.id.to_string(),
            CONSUMER_GROUP,
        );
        self.publisher
            .publish(
                ONTOLOGY_REINDEX_COMPLETED_V1,
                Some(job.id.as_bytes()),
                &payload,
                &lineage,
            )
            .await?;
        Ok(())
    }
}

// ─────────────────────────── Consumer loop ───────────────────────────

/// Subscribe and run the at-least-once consumer loop on the
/// requested-topic. One Kafka message ⇒ one full job (drives
/// pages until terminal). Offsets are committed AFTER the job
/// reaches a terminal status, so a crash mid-job replays the
/// message after restart and the coordinator picks up at the
/// persisted `resume_token`.
pub async fn run<S>(coordinator: Arc<Coordinator>, subscriber: S) -> Result<(), RuntimeError>
where
    S: DataSubscriber,
{
    // Resume any in-flight jobs left over from a crash before
    // starting to drain Kafka. This is the restart-safety
    // guarantee called out in the inventory doc §7.
    let resumable = coordinator.jobs.list_resumable().await?;
    if !resumable.is_empty() {
        tracing::info!(
            count = resumable.len(),
            "resuming in-flight jobs from a previous run"
        );
        for job in resumable {
            let coordinator = Arc::clone(&coordinator);
            tokio::spawn(async move {
                if let Err(e) = coordinator.run_job(job.id, None).await {
                    tracing::error!(job_id = %job.id, error = %e, "resumed job failed");
                }
            });
        }
    }

    subscriber.subscribe(SUBSCRIBE_TOPICS)?;
    tracing::info!(
        group = CONSUMER_GROUP,
        topics = ?SUBSCRIBE_TOPICS,
        "reindex-coordinator consumer loop started"
    );

    loop {
        let message = subscriber.recv().await?;
        let outcome = process_request_message(&coordinator, &message).await;
        match outcome {
            Ok(label) => {
                coordinator
                    .metrics
                    .requests_total
                    .with_label_values(&[label])
                    .inc();
                subscriber.commit(&message)?;
            }
            Err(e) => {
                // Do NOT commit — let Kafka redeliver. The state
                // machine + idempotency store make a re-run safe.
                coordinator
                    .metrics
                    .requests_total
                    .with_label_values(&["error"])
                    .inc();
                tracing::error!(error = %e, "reindex request failed; offset uncommitted");
                // Bounded sleep so a hot loop doesn't burn CPU
                // when the broker keeps redelivering the same
                // poison message before DLQ kicks in.
                sleep(Duration::from_secs(5)).await;
            }
        }
    }
}

async fn process_request_message(
    coordinator: &Arc<Coordinator>,
    message: &DataMessage,
) -> Result<&'static str, RuntimeError> {
    let Some(payload) = message.payload() else {
        tracing::warn!(
            topic = message.topic(),
            partition = message.partition(),
            offset = message.offset(),
            "skipping reindex request without payload"
        );
        return Ok("empty_payload");
    };
    let request: DecodedRequest = match decode_request(payload) {
        Ok(r) => r,
        Err(e) => {
            tracing::warn!(error = %e, "skipping malformed reindex request");
            return Ok("decode_error");
        }
    };
    let job_id = derive_job_id(&request.tenant_id, request.type_id.as_deref());
    let job = coordinator
        .jobs
        .upsert_queued(
            job_id,
            &request.tenant_id,
            request.type_id.as_deref(),
            request.page_size,
        )
        .await?;
    tracing::info!(
        %job_id,
        tenant = %request.tenant_id,
        type_id = ?request.type_id,
        page_size = request.page_size,
        existing_status = %job.status,
        "reindex request accepted"
    );
    if job.status.is_terminal() {
        // Re-run a finished job is the producer's responsibility;
        // we surface the terminal status in metrics and move on.
        return Ok("already_terminal");
    }
    coordinator.run_job(job_id, request.request_id).await?;
    Ok("completed")
}

// ────────────────────── Health / metrics HTTP ──────────────────────

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

pub fn health_addr_from_env(default_port: u16) -> Result<SocketAddr, RuntimeError> {
    let value = std::env::var("HEALTH_ADDR").unwrap_or_else(|_| format!("0.0.0.0:{default_port}"));
    value
        .parse::<SocketAddr>()
        .map_err(|_| RuntimeError::InvalidEnv {
            key: "HEALTH_ADDR",
            value,
            reason: "expected socket address, for example 0.0.0.0:8080",
        })
}

/// Tiny HTTP server exposing `/metrics`, `/health` and `/healthz`.
/// Same shape as the equivalent helper in `ontology-indexer::runtime`.
pub async fn serve_http(metrics: Arc<RuntimeMetrics>, addr: SocketAddr) -> std::io::Result<()> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    loop {
        let (mut stream, _) = listener.accept().await?;
        let metrics = Arc::clone(&metrics);
        tokio::spawn(async move {
            let mut buf = [0_u8; 1024];
            let read = tokio::time::timeout(Duration::from_secs(2), stream.read(&mut buf))
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

/// Build a `KafkaPublisher` from env. Convenience wrapper so
/// `main.rs` does not need to import `event_bus_data` directly.
pub fn kafka_publisher_from_env() -> Result<KafkaPublisher, RuntimeError> {
    KafkaPublisher::from_env(CONSUMER_GROUP).map_err(RuntimeError::Publish)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn subscribe_topics_pinned() {
        assert_eq!(SUBSCRIBE_TOPICS, &[ONTOLOGY_REINDEX_REQUESTED_V1]);
    }

    #[test]
    fn throttle_per_publish_sleep_zero_for_unbounded() {
        let t = Throttle {
            page_interval: Duration::ZERO,
            max_batches_per_second: 0,
        };
        assert!(t.per_publish_sleep().is_zero());
    }

    #[test]
    fn throttle_per_publish_sleep_under_rate_limit() {
        let t = Throttle {
            page_interval: Duration::from_millis(0),
            max_batches_per_second: 100,
        };
        assert_eq!(t.per_publish_sleep(), Duration::from_millis(10));
    }

    #[test]
    fn metrics_render_includes_pinned_names() {
        let m = RuntimeMetrics::new();
        // Force at least one observation per series so the render
        // emits the family.
        m.requests_total.with_label_values(&["completed"]).inc();
        m.batches_total.with_label_values(&["published"]).inc();
        m.records_total.with_label_values(&["published"]).inc_by(3);
        m.jobs_in_flight.set(1);

        let body = m.render().unwrap();
        assert!(body.contains("reindex_coordinator_requests_total"));
        assert!(body.contains("reindex_coordinator_batches_total"));
        assert!(body.contains("reindex_coordinator_records_total"));
        assert!(body.contains("reindex_coordinator_jobs_in_flight"));
    }
}
