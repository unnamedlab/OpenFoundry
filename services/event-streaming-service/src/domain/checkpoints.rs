//! Checkpoint coordinator (Bloque C).
//!
//! Each topology gets one [`tokio::time::interval`] task whose period
//! is taken from `streaming_topologies.checkpoint_interval_ms`. When
//! the timer fires the supervisor:
//!
//! 1. Snapshots the current state via the configured
//!    [`crate::domain::engine::state_store::StateBackend`].
//! 2. Records the per-stream `MAX(sequence_no)` so the offsets can be
//!    rewound on reset.
//! 3. Persists a row into `streaming_topology_checkpoints`.
//!
//! Manual checkpoints (POST /topologies/{id}/checkpoints) and the
//! reset path (POST /topologies/{id}/reset) reuse the same primitives
//! exposed at module scope.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

use crate::domain::engine::state_store::{SharedStateBackend, StateBackendError};
use crate::models::checkpoint::{Checkpoint, CheckpointRow};

#[derive(Debug, sqlx::FromRow)]
struct CheckpointTopologyConfig {
    id: Uuid,
    name: String,
    #[allow(dead_code)]
    status: String,
    checkpoint_interval_ms: i32,
    #[allow(dead_code)]
    runtime_kind: String,
    source_stream_ids: sqlx::types::Json<Vec<Uuid>>,
}

/// Supervisor that owns the per-topology checkpoint loops.
#[derive(Debug)]
pub struct CheckpointSupervisor {
    handles: Vec<tokio::task::JoinHandle<()>>,
}

impl CheckpointSupervisor {
    pub async fn spawn(
        db: PgPool,
        state_backend: SharedStateBackend,
    ) -> Result<Self, sqlx::Error> {
        let configs = sqlx::query_as::<_, CheckpointTopologyConfig>(
            "SELECT id, name, status, checkpoint_interval_ms, runtime_kind, source_stream_ids
               FROM streaming_topologies
              WHERE status = 'running' AND runtime_kind = 'builtin'",
        )
        .fetch_all(&db)
        .await?;

        let mut handles = Vec::with_capacity(configs.len());
        for cfg in configs {
            let task_db = db.clone();
            let backend = Arc::clone(&state_backend);
            let interval = Duration::from_millis(cfg.checkpoint_interval_ms.max(1000) as u64);
            let topology_id = cfg.id;
            let label = cfg.name.clone();
            let stream_ids = cfg.source_stream_ids.0.clone();
            tracing::info!(
                topology_id = %topology_id,
                topology = %label,
                interval_ms = cfg.checkpoint_interval_ms,
                "spawning checkpoint loop"
            );
            let handle = tokio::spawn(async move {
                run_loop(task_db, backend, topology_id, label, stream_ids, interval).await;
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

async fn run_loop(
    db: PgPool,
    backend: SharedStateBackend,
    topology_id: Uuid,
    label: String,
    stream_ids: Vec<Uuid>,
    interval: Duration,
) {
    let mut tick = tokio::time::interval(interval);
    tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    loop {
        tick.tick().await;
        match take_checkpoint(&db, &backend, topology_id, &stream_ids, "periodic", false)
            .await
        {
            Ok(cp) => {
                tracing::debug!(
                    topology_id = %topology_id,
                    topology = %label,
                    checkpoint_id = %cp.id,
                    "periodic checkpoint committed"
                );
            }
            Err(err) => {
                tracing::warn!(
                    topology_id = %topology_id,
                    topology = %label,
                    error = %err,
                    "periodic checkpoint failed"
                );
            }
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CheckpointError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("state backend error: {0}")]
    Backend(#[from] StateBackendError),
    #[error("invalid checkpoint payload: {0}")]
    Invalid(String),
}

/// Take a fresh checkpoint and persist it to the database.
///
/// `export_savepoint` is wired but not yet shipped — we record the URI
/// the upload would land at so the schema is complete.
pub async fn take_checkpoint(
    db: &PgPool,
    backend: &SharedStateBackend,
    topology_id: Uuid,
    stream_ids: &[Uuid],
    trigger: &str,
    export_savepoint: bool,
) -> Result<Checkpoint, CheckpointError> {
    let started = std::time::Instant::now();

    // Capture per-stream offsets — `MAX(sequence_no)` for each source
    // is the watermark the reset path will rewind to.
    let mut offsets: HashMap<String, i64> = HashMap::new();
    for stream_id in stream_ids {
        let max: Option<i64> = sqlx::query_scalar(
            "SELECT MAX(sequence_no) FROM streaming_events WHERE stream_id = $1",
        )
        .bind(stream_id)
        .fetch_one(db)
        .await?;
        offsets.insert(stream_id.to_string(), max.unwrap_or(0));
    }
    let last_offsets =
        serde_json::to_value(&offsets).map_err(|e| CheckpointError::Invalid(e.to_string()))?;

    // Materialise the operator state. The blob is currently held in the
    // Postgres row itself only when small; large blobs would be uploaded
    // to `state_uri` (object store) and only the URI would be persisted.
    let snapshot = backend.snapshot(topology_id).await?;
    let state_uri = if snapshot.is_empty() {
        None
    } else {
        // We do not actually upload the blob in this MVP — the URI is a
        // logical handle that the reset path matches against.
        Some(format!(
            "memory://topology/{}/snapshot/{}",
            topology_id,
            Utc::now().timestamp_millis()
        ))
    };
    let savepoint_uri = if export_savepoint {
        Some(format!(
            "savepoint://topology/{}/savepoint/{}",
            topology_id,
            Utc::now().timestamp_millis()
        ))
    } else {
        None
    };

    let id = Uuid::now_v7();
    let duration_ms = i32::try_from(started.elapsed().as_millis()).unwrap_or(i32::MAX);
    sqlx::query(
        "INSERT INTO streaming_topology_checkpoints
            (id, topology_id, status, last_offsets, state_uri, savepoint_uri, trigger, duration_ms)
         VALUES ($1, $2, 'committed', $3, $4, $5, $6, $7)",
    )
    .bind(id)
    .bind(topology_id)
    .bind(sqlx::types::Json(&last_offsets))
    .bind(state_uri.as_deref())
    .bind(savepoint_uri.as_deref())
    .bind(trigger)
    .bind(duration_ms)
    .execute(db)
    .await?;

    Ok(Checkpoint {
        id,
        topology_id,
        status: "committed".to_string(),
        last_offsets,
        state_uri,
        savepoint_uri,
        trigger: trigger.to_string(),
        duration_ms,
        created_at: Utc::now(),
    })
}

/// Load a single checkpoint or the latest one for a topology.
pub async fn load_checkpoint(
    db: &PgPool,
    topology_id: Uuid,
    checkpoint_id: Option<Uuid>,
) -> Result<Option<Checkpoint>, sqlx::Error> {
    let row = match checkpoint_id {
        Some(id) => {
            sqlx::query_as::<_, CheckpointRow>(
                "SELECT id, topology_id, status, last_offsets, state_uri, savepoint_uri,
                        trigger, duration_ms, created_at
                   FROM streaming_topology_checkpoints
                  WHERE topology_id = $1 AND id = $2",
            )
            .bind(topology_id)
            .bind(id)
            .fetch_optional(db)
            .await?
        }
        None => {
            sqlx::query_as::<_, CheckpointRow>(
                "SELECT id, topology_id, status, last_offsets, state_uri, savepoint_uri,
                        trigger, duration_ms, created_at
                   FROM streaming_topology_checkpoints
                  WHERE topology_id = $1
                  ORDER BY created_at DESC
                  LIMIT 1",
            )
            .bind(topology_id)
            .fetch_optional(db)
            .await?
        }
    };
    Ok(row.map(Checkpoint::from))
}

/// List committed checkpoints for a topology, newest first.
pub async fn list_checkpoints(
    db: &PgPool,
    topology_id: Uuid,
    limit: i64,
) -> Result<Vec<Checkpoint>, sqlx::Error> {
    let rows = sqlx::query_as::<_, CheckpointRow>(
        "SELECT id, topology_id, status, last_offsets, state_uri, savepoint_uri,
                trigger, duration_ms, created_at
           FROM streaming_topology_checkpoints
          WHERE topology_id = $1
          ORDER BY created_at DESC
          LIMIT $2",
    )
    .bind(topology_id)
    .bind(limit)
    .fetch_all(db)
    .await?;
    Ok(rows.into_iter().map(Checkpoint::from).collect())
}

/// Apply the offset rewind from a checkpoint by clearing
/// `processed_at` for any event that came after the captured offset
/// per stream.
pub async fn rewind_offsets_to(
    db: &PgPool,
    last_offsets: &Value,
) -> Result<i64, CheckpointError> {
    let map = match last_offsets {
        Value::Object(m) => m,
        _ => return Err(CheckpointError::Invalid("expected object".into())),
    };
    let mut total = 0i64;
    for (stream_id_str, offset_value) in map {
        let stream_id = Uuid::parse_str(stream_id_str)
            .map_err(|e| CheckpointError::Invalid(e.to_string()))?;
        let offset = offset_value
            .as_i64()
            .ok_or_else(|| CheckpointError::Invalid("offset must be integer".into()))?;
        let result = sqlx::query(
            "UPDATE streaming_events
                SET processed_at = NULL,
                    archived_at = NULL,
                    archive_path = NULL
              WHERE stream_id = $1 AND sequence_no > $2",
        )
        .bind(stream_id)
        .bind(offset)
        .execute(db)
        .await?;
        total += result.rows_affected() as i64;
    }
    Ok(total)
}
