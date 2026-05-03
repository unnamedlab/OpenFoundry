//! Checkpoint coordinator (Bloque C).
//!
//! Each topology gets one [`tokio::time::interval`] task whose period
//! is taken from `streaming_topologies.checkpoint_interval_ms`. When
//! the timer fires the supervisor:
//!
//! 1. Snapshots the current state via the configured
//!    [`crate::domain::engine::state_store::StateBackend`].
//! 2. Records the per-stream `MAX(sequence_no)` from the hot runtime
//!    store so resets can rewind to a stable offset.
//! 3. Persists the checkpoint metadata into the runtime store
//!    (memory + optional Cassandra), not Postgres.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

use crate::domain::engine::state_store::{SharedStateBackend, StateBackendError};
use crate::domain::runtime_store::{RuntimeStoreError, SharedRuntimeStore};
use crate::models::checkpoint::Checkpoint;

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
        runtime_store: SharedRuntimeStore,
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
            let backend = Arc::clone(&state_backend);
            let runtime = Arc::clone(&runtime_store);
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
                run_loop(runtime, backend, topology_id, label, stream_ids, interval).await;
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
    runtime_store: SharedRuntimeStore,
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
        match take_checkpoint(
            &runtime_store,
            &backend,
            topology_id,
            &stream_ids,
            "periodic",
            false,
        )
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
    #[error("state backend error: {0}")]
    Backend(#[from] StateBackendError),
    #[error("runtime store error: {0}")]
    Runtime(#[from] RuntimeStoreError),
    #[error("invalid checkpoint payload: {0}")]
    Invalid(String),
}

/// Take a fresh checkpoint and persist it to the runtime store.
pub async fn take_checkpoint(
    runtime_store: &SharedRuntimeStore,
    backend: &SharedStateBackend,
    topology_id: Uuid,
    stream_ids: &[Uuid],
    trigger: &str,
    export_savepoint: bool,
) -> Result<Checkpoint, CheckpointError> {
    let started = std::time::Instant::now();

    let mut offsets: HashMap<String, i64> = HashMap::new();
    for stream_id in stream_ids {
        let max = runtime_store.max_sequence_no(*stream_id).await?;
        offsets.insert(stream_id.to_string(), max);
    }
    let last_offsets =
        serde_json::to_value(&offsets).map_err(|e| CheckpointError::Invalid(e.to_string()))?;

    let snapshot = backend.snapshot(topology_id).await?;
    let state_uri = if snapshot.is_empty() {
        None
    } else {
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

    let checkpoint = Checkpoint {
        id: Uuid::now_v7(),
        topology_id,
        status: "committed".to_string(),
        last_offsets,
        state_uri,
        savepoint_uri,
        trigger: trigger.to_string(),
        duration_ms: i32::try_from(started.elapsed().as_millis()).unwrap_or(i32::MAX),
        created_at: Utc::now(),
    };
    runtime_store.insert_checkpoint(checkpoint.clone()).await?;

    Ok(checkpoint)
}

pub async fn load_checkpoint(
    runtime_store: &SharedRuntimeStore,
    topology_id: Uuid,
    checkpoint_id: Option<Uuid>,
) -> Result<Option<Checkpoint>, CheckpointError> {
    runtime_store
        .load_checkpoint(topology_id, checkpoint_id)
        .await
        .map_err(Into::into)
}

pub async fn list_checkpoints(
    runtime_store: &SharedRuntimeStore,
    topology_id: Uuid,
    limit: usize,
) -> Result<Vec<Checkpoint>, CheckpointError> {
    runtime_store
        .list_checkpoints(topology_id, limit)
        .await
        .map_err(Into::into)
}

/// Apply the offset rewind from a checkpoint by clearing processed and
/// archived flags for every event after the captured offset per stream.
pub async fn rewind_offsets_to(
    runtime_store: &SharedRuntimeStore,
    last_offsets: &Value,
) -> Result<i64, CheckpointError> {
    let map = match last_offsets {
        Value::Object(m) => m,
        _ => return Err(CheckpointError::Invalid("expected object".into())),
    };
    let mut total = 0i64;
    for (stream_id_str, offset_value) in map {
        let stream_id =
            Uuid::parse_str(stream_id_str).map_err(|e| CheckpointError::Invalid(e.to_string()))?;
        let offset = offset_value
            .as_i64()
            .ok_or_else(|| CheckpointError::Invalid("offset must be integer".into()))?;
        total += runtime_store
            .restore_events(&[stream_id], Some(offset + 1))
            .await?;
    }
    Ok(total)
}
