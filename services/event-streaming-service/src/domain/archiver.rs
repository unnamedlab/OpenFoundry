//! Cold-tier archiver.
//!
//! The archiver drains processed events from the runtime store hot tier
//! into the configured dataset writer and records the cold snapshot index
//! back into the runtime store (memory + optional Cassandra).

use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::domain::runtime_store::{ColdArchiveRecord, SharedRuntimeStore};
use crate::storage::{DatasetSnapshot, DatasetWriter, WriterError};

const ROWS_PER_MB: i64 = 1024;
const HARD_ROW_CAP: i64 = 1_000_000;

#[derive(Debug, Clone, sqlx::FromRow)]
struct ArchiverStreamConfig {
    id: Uuid,
    name: String,
    archive_interval_seconds: i32,
    target_file_size_mb: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ArchivedEvent {
    sequence_no: i64,
    event_time: chrono::DateTime<Utc>,
    payload: serde_json::Value,
}

#[derive(Debug)]
pub struct ArchiverSupervisor {
    handles: Vec<tokio::task::JoinHandle<()>>,
}

impl ArchiverSupervisor {
    pub async fn spawn(
        runtime_store: SharedRuntimeStore,
        dataset_writer: Arc<dyn DatasetWriter>,
        http_client: reqwest::Client,
        dataset_service_url: String,
        db: sqlx::PgPool,
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
            let runtime = Arc::clone(&runtime_store);
            let writer = Arc::clone(&dataset_writer);
            let http = http_client.clone();
            let url = dataset_service_url.clone();
            let stream_label = config.name.clone();
            let stream_id = config.id;
            let interval = Duration::from_secs(config.archive_interval_seconds.max(5) as u64);
            let row_cap =
                ((config.target_file_size_mb as i64) * ROWS_PER_MB).clamp(1, HARD_ROW_CAP) as usize;
            tracing::info!(
                stream_id = %stream_id,
                stream = %stream_label,
                interval_secs = config.archive_interval_seconds,
                row_cap,
                "spawning cold-tier archiver"
            );

            let handle = tokio::spawn(async move {
                run_archiver_loop(
                    runtime,
                    writer,
                    http,
                    url,
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

    pub fn shutdown(&self) {
        for handle in &self.handles {
            handle.abort();
        }
    }
}

#[allow(clippy::too_many_arguments)]
async fn run_archiver_loop(
    runtime_store: SharedRuntimeStore,
    dataset_writer: Arc<dyn DatasetWriter>,
    http_client: reqwest::Client,
    dataset_service_url: String,
    stream_id: Uuid,
    stream_label: String,
    interval: Duration,
    row_cap: usize,
) {
    let mut tick = tokio::time::interval(interval);
    tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    loop {
        tick.tick().await;
        match flush_once(
            &runtime_store,
            &dataset_writer,
            &http_client,
            &dataset_service_url,
            stream_id,
            row_cap,
        )
        .await
        {
            Ok(0) => tracing::trace!(stream_id = %stream_id, "archiver tick: no eligible events"),
            Ok(n) => tracing::info!(
                stream_id = %stream_id,
                stream = %stream_label,
                rows = n,
                "archiver tick: flushed batch"
            ),
            Err(err) => tracing::warn!(
                stream_id = %stream_id,
                stream = %stream_label,
                error = %err,
                "archiver tick failed; will retry on next interval"
            ),
        }
    }
}

async fn flush_once(
    runtime_store: &SharedRuntimeStore,
    dataset_writer: &Arc<dyn DatasetWriter>,
    http_client: &reqwest::Client,
    dataset_service_url: &str,
    stream_id: Uuid,
    row_cap: usize,
) -> Result<usize, ArchiverError> {
    let retention_cutoff = Utc::now();
    let rows = runtime_store
        .archive_candidates(stream_id, retention_cutoff, row_cap)
        .await
        .map_err(ArchiverError::Runtime)?;

    if rows.is_empty() {
        return Ok(0);
    }

    let archived = rows
        .iter()
        .map(|row| ArchivedEvent {
            sequence_no: row.sequence_no,
            event_time: row.event_time,
            payload: row.payload.clone(),
        })
        .collect::<Vec<_>>();
    let last_seq = archived.last().expect("non-empty checked").sequence_no;
    let row_count = archived.len();

    let mut payload_bytes = Vec::with_capacity(row_count * 256);
    for event in &archived {
        serde_json::to_writer(&mut payload_bytes, event)
            .map_err(|e| ArchiverError::Serialize(e.to_string()))?;
        payload_bytes.push(b'\n');
    }

    let snapshot_id = format!("{stream_id}-{}", Utc::now().timestamp_millis());
    let table_name = format!("stream_{}", stream_id.simple());
    let snapshot = DatasetSnapshot::new(
        table_name,
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

    runtime_store
        .record_cold_archive(
            ColdArchiveRecord {
                stream_id,
                snapshot_id: snapshot_id.clone(),
                last_offset: last_seq,
                snapshot_at: Utc::now(),
                dataset_id: None,
                parquet_path: outcome.location.clone(),
                row_count: row_count as i64,
                bytes_on_disk: payload_bytes.len() as i64,
            },
            &rows.iter().map(|row| row.id).collect::<Vec<_>>(),
        )
        .await
        .map_err(ArchiverError::Runtime)?;

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
    match http_client
        .post(&notify_url)
        .json(&notify_payload)
        .send()
        .await
    {
        Ok(resp) if resp.status().is_success() => {
            tracing::debug!(stream_id = %stream_id, "data-asset-catalog notified of new snapshot");
        }
        Ok(resp) => {
            tracing::warn!(stream_id = %stream_id, status = %resp.status(), "data-asset-catalog notification rejected");
        }
        Err(err) => {
            tracing::warn!(stream_id = %stream_id, error = %err, "data-asset-catalog notification failed");
        }
    }

    Ok(row_count)
}

#[derive(Debug, thiserror::Error)]
enum ArchiverError {
    #[error("runtime store error: {0}")]
    Runtime(crate::domain::runtime_store::RuntimeStoreError),
    #[error("dataset writer error: {0}")]
    Writer(WriterError),
    #[error("serialisation error: {0}")]
    Serialize(String),
}
