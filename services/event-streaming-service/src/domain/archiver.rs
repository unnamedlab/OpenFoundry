//! Cold-tier archiver.
//!
//! For every active stream the service spawns one `tokio::time::interval`
//! task that drains the hot tier (`streaming_events` rows beyond the
//! last archived offset) into the cold tier (Iceberg + the data-asset
//! catalog) using the configured [`crate::storage::DatasetWriter`].
//!
//! The cadence per stream is governed by `archive_interval_seconds`
//! (default 120 s). The flush size is governed by `target_file_size_mb`
//! (default 128 MB) which we approximate as a row count cap so the
//! archiver does not need to know the encoded payload size up front.
//!
//! Every successful flush records a row in `streaming_cold_archives` so
//! the read path (`handlers::streams::read_stream`) can locate the
//! Parquet snapshot for a given offset range and so the next tick knows
//! where to resume from. Failures are logged and retried on the next
//! tick — we do not advance `last_offset` if the writer call fails.

use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::PgPool;
use sqlx::types::Json as SqlJson;
use uuid::Uuid;

use crate::storage::{DatasetSnapshot, DatasetWriter, WriterError};

/// Soft cap on rows flushed in a single tick, derived from
/// `target_file_size_mb`. We assume ~1 KB/row (typical JSON event).
const ROWS_PER_MB: i64 = 1024;
/// Hard upper bound to keep memory pressure predictable even when the
/// operator sets `target_file_size_mb` to the schema maximum (4096 MB).
const HARD_ROW_CAP: i64 = 1_000_000;

/// Per-stream configuration the supervisor reads from
/// `streaming_streams` once at startup. Changes require a service
/// restart for now; live reload is captured in the post-MVP backlog.
#[derive(Debug, Clone, sqlx::FromRow)]
struct ArchiverStreamConfig {
    id: Uuid,
    name: String,
    archive_interval_seconds: i32,
    target_file_size_mb: i32,
}

/// Compact JSON representation of a single archived event.
///
/// We keep it stable here so the Parquet schema produced by the
/// `IcebergDatasetWriter` is the same regardless of the source schema.
#[derive(Debug, Clone, Serialize, Deserialize)]
struct ArchivedEvent {
    sequence_no: i64,
    event_time: DateTime<Utc>,
    payload: serde_json::Value,
}

/// Public archiver supervisor. Holds the [`tokio::task::JoinHandle`]s
/// for the per-stream loops so the service can shut them down cleanly
/// on `SIGTERM`.
#[derive(Debug)]
pub struct ArchiverSupervisor {
    handles: Vec<tokio::task::JoinHandle<()>>,
}

impl ArchiverSupervisor {
    /// Spawn one archiver task per active stream. Returns immediately
    /// once the tasks are running.
    pub async fn spawn(
        db: PgPool,
        dataset_writer: Arc<dyn DatasetWriter>,
        http_client: reqwest::Client,
        dataset_service_url: String,
    ) -> Result<Self, sqlx::Error> {
        let configs = sqlx::query_as::<_, ArchiverStreamConfig>(
            "SELECT id, name, archive_interval_seconds, target_file_size_mb
             FROM streaming_streams
             WHERE status = 'active'",
        )
        .fetch_all(&db)
        .await?;

        let mut handles = Vec::with_capacity(configs.len());
        for config in configs {
            let task_db = db.clone();
            let task_writer = Arc::clone(&dataset_writer);
            let task_http = http_client.clone();
            let task_url = dataset_service_url.clone();
            let stream_label = config.name.clone();
            let stream_id = config.id;
            let interval =
                Duration::from_secs(config.archive_interval_seconds.max(5) as u64);
            let row_cap = ((config.target_file_size_mb as i64) * ROWS_PER_MB)
                .clamp(1, HARD_ROW_CAP);
            tracing::info!(
                stream_id = %stream_id,
                stream = %stream_label,
                interval_secs = config.archive_interval_seconds,
                row_cap,
                "spawning cold-tier archiver"
            );

            let handle = tokio::spawn(async move {
                run_archiver_loop(
                    task_db,
                    task_writer,
                    task_http,
                    task_url,
                    stream_id,
                    stream_label,
                    interval,
                    row_cap,
                )
                .await;
            });
            handles.push(handle);
        }

        Ok(Self { handles })
    }

    /// Abort every per-stream loop. Called from the main tokio::select!
    /// when a peer server returns.
    pub fn shutdown(&self) {
        for handle in &self.handles {
            handle.abort();
        }
    }
}

#[allow(clippy::too_many_arguments)]
async fn run_archiver_loop(
    db: PgPool,
    dataset_writer: Arc<dyn DatasetWriter>,
    http_client: reqwest::Client,
    dataset_service_url: String,
    stream_id: Uuid,
    stream_label: String,
    interval: Duration,
    row_cap: i64,
) {
    let mut tick = tokio::time::interval(interval);
    tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    loop {
        tick.tick().await;
        match flush_once(
            &db,
            &dataset_writer,
            &http_client,
            &dataset_service_url,
            stream_id,
            row_cap,
        )
        .await
        {
            Ok(0) => {
                tracing::trace!(stream_id = %stream_id, "archiver tick: no new events");
            }
            Ok(n) => {
                tracing::info!(
                    stream_id = %stream_id,
                    stream = %stream_label,
                    rows = n,
                    "archiver tick: flushed batch"
                );
            }
            Err(err) => {
                tracing::warn!(
                    stream_id = %stream_id,
                    stream = %stream_label,
                    error = %err,
                    "archiver tick failed; will retry on next interval"
                );
            }
        }
    }
}

/// Flush one batch of events for a single stream. Returns the number of
/// rows persisted. Idempotent only at the snapshot id level — rerunning
/// the loop without advancing `last_offset` would double-write.
async fn flush_once(
    db: &PgPool,
    dataset_writer: &Arc<dyn DatasetWriter>,
    http_client: &reqwest::Client,
    dataset_service_url: &str,
    stream_id: Uuid,
    row_cap: i64,
) -> Result<usize, ArchiverError> {
    let last_offset: i64 = sqlx::query_scalar(
        "SELECT COALESCE(MAX(last_offset), 0) FROM streaming_cold_archives WHERE stream_id = $1",
    )
    .bind(stream_id)
    .fetch_one(db)
    .await
    .map_err(ArchiverError::Db)?;

    let rows: Vec<(i64, DateTime<Utc>, SqlJson<serde_json::Value>)> = sqlx::query_as(
        "SELECT sequence_no, event_time, payload
           FROM streaming_events
          WHERE stream_id = $1 AND sequence_no > $2
          ORDER BY sequence_no ASC
          LIMIT $3",
    )
    .bind(stream_id)
    .bind(last_offset)
    .bind(row_cap)
    .fetch_all(db)
    .await
    .map_err(ArchiverError::Db)?;

    if rows.is_empty() {
        return Ok(0);
    }

    let archived: Vec<ArchivedEvent> = rows
        .into_iter()
        .map(|(sequence_no, event_time, payload)| ArchivedEvent {
            sequence_no,
            event_time,
            payload: payload.0,
        })
        .collect();
    let last_seq = archived.last().expect("non-empty checked").sequence_no;
    let row_count = archived.len();

    // Serialise the batch as JSON-Lines bytes. The Iceberg writer wraps
    // these into a single Parquet data file via its own arrow encoder so
    // the archiver stays decoupled from the parquet schema definition.
    let mut payload_bytes = Vec::with_capacity(row_count * 256);
    for event in &archived {
        serde_json::to_writer(&mut payload_bytes, event)
            .map_err(|e| ArchiverError::Serialize(e.to_string()))?;
        payload_bytes.push(b'\n');
    }

    let snapshot_id = format!(
        "{stream_id}-{}",
        Utc::now().timestamp_millis()
    );
    let table_name = format!("stream_{}", stream_id.simple());
    let snapshot = DatasetSnapshot::new(
        table_name.clone(),
        snapshot_id.clone(),
        payload_bytes.clone().into(),
    )
    .with_metadata(serde_json::json!({
        "stream_id": stream_id,
        "first_offset": archived.first().map(|e| e.sequence_no),
        "last_offset": last_seq,
        "row_count": row_count,
    }));

    let outcome = dataset_writer
        .append(snapshot)
        .await
        .map_err(ArchiverError::Writer)?;

    sqlx::query(
        "INSERT INTO streaming_cold_archives
            (id, stream_id, snapshot_id, last_offset, snapshot_at,
             dataset_id, parquet_path, row_count, bytes_on_disk)
         VALUES ($1, $2, $3, $4, $5, NULL, $6, $7, $8)
         ON CONFLICT (stream_id, snapshot_id) DO NOTHING",
    )
    .bind(Uuid::now_v7())
    .bind(stream_id)
    .bind(&snapshot_id)
    .bind(last_seq)
    .bind(Utc::now())
    .bind(&outcome.location)
    .bind(row_count as i64)
    .bind(payload_bytes.len() as i64)
    .execute(db)
    .await
    .map_err(ArchiverError::Db)?;

    // Best-effort notification to the data-asset-catalog so analysts
    // can discover the new snapshot. A non-2xx response is logged but
    // does not fail the flush — the local row in
    // `streaming_cold_archives` is the source of truth.
    let notify_url = format!(
        "{}/api/v1/datasets/streaming/{}/snapshots",
        dataset_service_url.trim_end_matches('/'),
        stream_id
    );
    let notify_payload = serde_json::json!({
        "snapshot_id": snapshot_id,
        "location": outcome.location,
        "backend": outcome.backend,
        "row_count": row_count,
        "last_offset": last_seq,
    });
    match http_client.post(&notify_url).json(&notify_payload).send().await {
        Ok(resp) if resp.status().is_success() => {
            tracing::debug!(stream_id = %stream_id, "data-asset-catalog notified of snapshot {snapshot_id}");
        }
        Ok(resp) => {
            tracing::warn!(
                stream_id = %stream_id,
                status = %resp.status(),
                "data-asset-catalog notification rejected"
            );
        }
        Err(err) => {
            tracing::warn!(
                stream_id = %stream_id,
                error = %err,
                "data-asset-catalog notification failed (will not be retried for this snapshot)"
            );
        }
    }

    Ok(row_count)
}

#[derive(Debug, thiserror::Error)]
enum ArchiverError {
    #[error("database error: {0}")]
    Db(sqlx::Error),
    #[error("dataset writer error: {0}")]
    Writer(WriterError),
    #[error("serialisation error: {0}")]
    Serialize(String),
}
