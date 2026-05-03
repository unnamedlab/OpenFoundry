use std::collections::HashMap;
use std::sync::Arc;

use async_trait::async_trait;
use cassandra_kernel::Migration;
use cassandra_kernel::scylla::{Session, frame::value::CqlTimestamp};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tokio::sync::RwLock;
use uuid::Uuid;

use crate::models::checkpoint::Checkpoint;

const KEYSPACE: &str = "streaming_runtime";

const RUNTIME_KEYSPACE_DDL: &str = "\
CREATE KEYSPACE IF NOT EXISTS streaming_runtime \
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}";

const TOPOLOGY_OFFSETS_DDL: &str = "\
CREATE TABLE IF NOT EXISTS streaming_runtime.topology_offsets ( \
    topology_id       uuid, \
    stream_id         uuid, \
    last_sequence_no  bigint, \
    updated_at        timestamp, \
    PRIMARY KEY ((topology_id), stream_id) \
)";

const TOPOLOGY_CHECKPOINTS_BY_TOPOLOGY_DDL: &str = "\
CREATE TABLE IF NOT EXISTS streaming_runtime.topology_checkpoints_by_topology ( \
    topology_id   uuid, \
    created_at    timestamp, \
    checkpoint_id uuid, \
    status        text, \
    last_offsets  text, \
    state_uri     text, \
    savepoint_uri text, \
    trigger       text, \
    duration_ms   int, \
    PRIMARY KEY ((topology_id), created_at, checkpoint_id) \
) WITH CLUSTERING ORDER BY (created_at DESC, checkpoint_id ASC)";

const TOPOLOGY_CHECKPOINTS_BY_ID_DDL: &str = "\
CREATE TABLE IF NOT EXISTS streaming_runtime.topology_checkpoints_by_id ( \
    topology_id   uuid, \
    checkpoint_id uuid, \
    created_at    timestamp, \
    status        text, \
    last_offsets  text, \
    state_uri     text, \
    savepoint_uri text, \
    trigger       text, \
    duration_ms   int, \
    PRIMARY KEY ((topology_id), checkpoint_id) \
)";

const COLD_ARCHIVES_BY_STREAM_DDL: &str = "\
CREATE TABLE IF NOT EXISTS streaming_runtime.cold_archives_by_stream ( \
    stream_id      uuid, \
    snapshot_at    timestamp, \
    snapshot_id    text, \
    last_offset    bigint, \
    dataset_id     uuid, \
    parquet_path   text, \
    row_count      bigint, \
    bytes_on_disk  bigint, \
    PRIMARY KEY ((stream_id), snapshot_at, snapshot_id) \
) WITH CLUSTERING ORDER BY (snapshot_at DESC, snapshot_id ASC)";

const MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "streaming_runtime_checkpoint_and_archive_index",
    statements: &[
        TOPOLOGY_OFFSETS_DDL,
        TOPOLOGY_CHECKPOINTS_BY_TOPOLOGY_DDL,
        TOPOLOGY_CHECKPOINTS_BY_ID_DDL,
        COLD_ARCHIVES_BY_STREAM_DDL,
    ],
}];

#[derive(Debug, thiserror::Error)]
pub enum RuntimeStoreError {
    #[error("runtime store unavailable: {0}")]
    Unavailable(String),
    #[error("runtime store serialisation error: {0}")]
    Serialize(String),
    #[error("cassandra runtime store error: {0}")]
    Cassandra(#[from] cassandra_kernel::KernelError),
    #[error("cassandra query error: {0}")]
    Query(#[from] cassandra_kernel::scylla::transport::errors::QueryError),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RuntimeEvent {
    pub id: Uuid,
    pub stream_id: Uuid,
    pub sequence_no: i64,
    pub payload: Value,
    pub event_time: DateTime<Utc>,
    pub processed_at: Option<DateTime<Utc>>,
    pub archived_at: Option<DateTime<Utc>>,
    pub archive_path: Option<String>,
}

#[derive(Debug, Clone)]
pub struct StreamCheckpointOffset {
    pub stream_id: Uuid,
    pub last_sequence_no: i64,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ColdArchiveRecord {
    pub stream_id: Uuid,
    pub snapshot_id: String,
    pub last_offset: i64,
    pub snapshot_at: DateTime<Utc>,
    pub dataset_id: Option<Uuid>,
    pub parquet_path: String,
    pub row_count: i64,
    pub bytes_on_disk: i64,
}

#[derive(Debug, Clone, Copy, Default)]
pub struct StreamActivity {
    pub backlog: i64,
    pub recent_events: i64,
    pub oldest_event_time: Option<DateTime<Utc>>,
    pub newest_event_time: Option<DateTime<Utc>>,
}

#[async_trait]
pub trait RuntimeStore: Send + Sync + std::fmt::Debug {
    async fn append_event(
        &self,
        stream_id: Uuid,
        payload: Value,
        event_time: DateTime<Utc>,
    ) -> Result<RuntimeEvent, RuntimeStoreError>;

    async fn list_events_since(
        &self,
        stream_id: Uuid,
        after_sequence_no: i64,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError>;

    async fn list_recent_events(
        &self,
        stream_id: Uuid,
        limit: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError>;

    async fn list_events_between(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError>;

    async fn live_event_count_since(&self, since: DateTime<Utc>) -> Result<i64, RuntimeStoreError>;

    async fn stream_activity(
        &self,
        recent_since: DateTime<Utc>,
    ) -> Result<HashMap<Uuid, StreamActivity>, RuntimeStoreError>;

    async fn mark_events_processed(&self, event_ids: &[Uuid]) -> Result<(), RuntimeStoreError>;

    async fn restore_events(
        &self,
        stream_ids: &[Uuid],
        from_sequence_no: Option<i64>,
    ) -> Result<i64, RuntimeStoreError>;

    async fn archive_candidates(
        &self,
        stream_id: Uuid,
        retention_cutoff: DateTime<Utc>,
        row_cap: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError>;

    async fn record_cold_archive(
        &self,
        archive: ColdArchiveRecord,
        archived_event_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError>;

    async fn cold_watermark(
        &self,
        stream_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError>;

    async fn list_cold_archives(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<ColdArchiveRecord>, RuntimeStoreError>;

    async fn max_sequence_no(&self, stream_id: Uuid) -> Result<i64, RuntimeStoreError>;

    async fn save_topology_offsets(
        &self,
        topology_id: Uuid,
        offsets: HashMap<Uuid, i64>,
    ) -> Result<(), RuntimeStoreError>;

    async fn set_topology_offset(
        &self,
        topology_id: Uuid,
        stream_id: Uuid,
        sequence_no: i64,
    ) -> Result<(), RuntimeStoreError>;

    async fn clear_topology_offsets(
        &self,
        topology_id: Uuid,
        stream_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError>;

    async fn load_topology_offsets(
        &self,
        topology_id: Uuid,
    ) -> Result<HashMap<Uuid, StreamCheckpointOffset>, RuntimeStoreError>;

    async fn insert_checkpoint(&self, checkpoint: Checkpoint) -> Result<(), RuntimeStoreError>;

    async fn load_checkpoint(
        &self,
        topology_id: Uuid,
        checkpoint_id: Option<Uuid>,
    ) -> Result<Option<Checkpoint>, RuntimeStoreError>;

    async fn list_checkpoints(
        &self,
        topology_id: Uuid,
        limit: usize,
    ) -> Result<Vec<Checkpoint>, RuntimeStoreError>;

    async fn latest_checkpoint_at(
        &self,
        topology_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError>;
}

pub type SharedRuntimeStore = Arc<dyn RuntimeStore>;

#[derive(Debug, Default)]
struct MemoryRuntimeState {
    next_sequence_by_stream: HashMap<Uuid, i64>,
    events_by_stream: HashMap<Uuid, Vec<RuntimeEvent>>,
    topology_offsets: HashMap<Uuid, HashMap<Uuid, StreamCheckpointOffset>>,
    checkpoints: HashMap<Uuid, Vec<Checkpoint>>,
    cold_archives: HashMap<Uuid, Vec<ColdArchiveRecord>>,
}

#[derive(Debug, Default)]
pub struct MemoryRuntimeStore {
    inner: RwLock<MemoryRuntimeState>,
}

impl MemoryRuntimeStore {
    pub fn new() -> Self {
        Self::default()
    }
}

#[derive(Debug, Clone)]
pub struct CassandraRuntimeStore {
    session: Arc<Session>,
}

impl CassandraRuntimeStore {
    pub fn new(session: Arc<Session>) -> Self {
        Self { session }
    }

    pub async fn migrate(&self) -> Result<(), RuntimeStoreError> {
        self.session.query(RUNTIME_KEYSPACE_DDL, ()).await?;
        cassandra_kernel::migrate::apply(&self.session, KEYSPACE, MIGRATIONS).await?;
        Ok(())
    }

    async fn save_offsets(
        &self,
        topology_id: Uuid,
        offsets: &HashMap<Uuid, i64>,
    ) -> Result<(), RuntimeStoreError> {
        let updated_at = cql_ts(Utc::now());
        for (stream_id, sequence_no) in offsets {
            self.session
                .query(
                    "INSERT INTO streaming_runtime.topology_offsets \
                     (topology_id, stream_id, last_sequence_no, updated_at) \
                     VALUES (?, ?, ?, ?)",
                    (topology_id, *stream_id, *sequence_no, updated_at),
                )
                .await?;
        }
        Ok(())
    }

    async fn set_offset(
        &self,
        topology_id: Uuid,
        stream_id: Uuid,
        sequence_no: i64,
    ) -> Result<(), RuntimeStoreError> {
        self.session
            .query(
                "INSERT INTO streaming_runtime.topology_offsets \
                 (topology_id, stream_id, last_sequence_no, updated_at) \
                 VALUES (?, ?, ?, ?)",
                (topology_id, stream_id, sequence_no, cql_ts(Utc::now())),
            )
            .await?;
        Ok(())
    }

    async fn clear_offsets(
        &self,
        topology_id: Uuid,
        stream_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError> {
        for stream_id in stream_ids {
            self.session
                .query(
                    "DELETE FROM streaming_runtime.topology_offsets \
                     WHERE topology_id = ? AND stream_id = ?",
                    (topology_id, *stream_id),
                )
                .await?;
        }
        Ok(())
    }

    async fn load_offsets(
        &self,
        topology_id: Uuid,
    ) -> Result<HashMap<Uuid, StreamCheckpointOffset>, RuntimeStoreError> {
        let result = self
            .session
            .query(
                "SELECT stream_id, last_sequence_no, updated_at \
                 FROM streaming_runtime.topology_offsets \
                 WHERE topology_id = ?",
                (topology_id,),
            )
            .await?;
        let mut items = HashMap::new();
        for row in result.rows_typed_or_empty::<(Uuid, i64, CqlTimestamp)>() {
            let (stream_id, last_sequence_no, updated_at) = row.map_err(|e| {
                RuntimeStoreError::Unavailable(format!("offset row decode failed: {e}"))
            })?;
            items.insert(
                stream_id,
                StreamCheckpointOffset {
                    stream_id,
                    last_sequence_no,
                    updated_at: timestamp_to_utc(updated_at),
                },
            );
        }
        Ok(items)
    }

    async fn insert_checkpoint(&self, checkpoint: &Checkpoint) -> Result<(), RuntimeStoreError> {
        let last_offsets = serde_json::to_string(&checkpoint.last_offsets)
            .map_err(|e| RuntimeStoreError::Serialize(e.to_string()))?;
        let created_at = cql_ts(checkpoint.created_at);
        self.session
            .query(
                "INSERT INTO streaming_runtime.topology_checkpoints_by_topology \
                 (topology_id, created_at, checkpoint_id, status, last_offsets, \
                  state_uri, savepoint_uri, trigger, duration_ms) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    checkpoint.topology_id,
                    created_at,
                    checkpoint.id,
                    checkpoint.status.as_str(),
                    last_offsets.as_str(),
                    checkpoint.state_uri.as_deref(),
                    checkpoint.savepoint_uri.as_deref(),
                    checkpoint.trigger.as_str(),
                    checkpoint.duration_ms,
                ),
            )
            .await?;
        self.session
            .query(
                "INSERT INTO streaming_runtime.topology_checkpoints_by_id \
                 (topology_id, checkpoint_id, created_at, status, last_offsets, \
                  state_uri, savepoint_uri, trigger, duration_ms) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    checkpoint.topology_id,
                    checkpoint.id,
                    created_at,
                    checkpoint.status.as_str(),
                    last_offsets.as_str(),
                    checkpoint.state_uri.as_deref(),
                    checkpoint.savepoint_uri.as_deref(),
                    checkpoint.trigger.as_str(),
                    checkpoint.duration_ms,
                ),
            )
            .await?;
        Ok(())
    }

    async fn load_checkpoint(
        &self,
        topology_id: Uuid,
        checkpoint_id: Option<Uuid>,
    ) -> Result<Option<Checkpoint>, RuntimeStoreError> {
        if let Some(checkpoint_id) = checkpoint_id {
            let result = self
                .session
                .query(
                    "SELECT checkpoint_id, status, last_offsets, state_uri, \
                     savepoint_uri, trigger, duration_ms, created_at \
                     FROM streaming_runtime.topology_checkpoints_by_id \
                     WHERE topology_id = ? AND checkpoint_id = ?",
                    (topology_id, checkpoint_id),
                )
                .await?;
            for row in result.rows_typed_or_empty::<(
                Uuid,
                String,
                String,
                Option<String>,
                Option<String>,
                String,
                i32,
                CqlTimestamp,
            )>() {
                let (
                    id,
                    status,
                    last_offsets,
                    state_uri,
                    savepoint_uri,
                    trigger,
                    duration_ms,
                    created_at,
                ) = row.map_err(|e| {
                    RuntimeStoreError::Unavailable(format!("checkpoint row decode failed: {e}"))
                })?;
                return Ok(Some(build_checkpoint(
                    id,
                    topology_id,
                    &status,
                    &last_offsets,
                    state_uri,
                    savepoint_uri,
                    &trigger,
                    duration_ms,
                    timestamp_to_utc(created_at),
                )?));
            }
            return Ok(None);
        }

        let items = self.list_checkpoints(topology_id, 1).await?;
        Ok(items.into_iter().next())
    }

    async fn list_checkpoints(
        &self,
        topology_id: Uuid,
        limit: usize,
    ) -> Result<Vec<Checkpoint>, RuntimeStoreError> {
        let result = self
            .session
            .query(
                "SELECT checkpoint_id, status, last_offsets, state_uri, \
                 savepoint_uri, trigger, duration_ms, created_at \
                 FROM streaming_runtime.topology_checkpoints_by_topology \
                 WHERE topology_id = ? LIMIT ?",
                (topology_id, limit as i32),
            )
            .await?;
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(
            Uuid,
            String,
            String,
            Option<String>,
            Option<String>,
            String,
            i32,
            CqlTimestamp,
        )>() {
            let (
                id,
                status,
                last_offsets,
                state_uri,
                savepoint_uri,
                trigger,
                duration_ms,
                created_at,
            ) = row.map_err(|e| {
                RuntimeStoreError::Unavailable(format!("checkpoint row decode failed: {e}"))
            })?;
            items.push(build_checkpoint(
                id,
                topology_id,
                &status,
                &last_offsets,
                state_uri,
                savepoint_uri,
                &trigger,
                duration_ms,
                timestamp_to_utc(created_at),
            )?);
        }
        items.sort_by_key(|item| std::cmp::Reverse(item.created_at));
        items.truncate(limit);
        Ok(items)
    }

    async fn latest_checkpoint_at(
        &self,
        topology_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError> {
        let item = self.list_checkpoints(topology_id, 1).await?;
        Ok(item.into_iter().next().map(|cp| cp.created_at))
    }

    async fn insert_cold_archive(
        &self,
        archive: &ColdArchiveRecord,
    ) -> Result<(), RuntimeStoreError> {
        self.session
            .query(
                "INSERT INTO streaming_runtime.cold_archives_by_stream \
                 (stream_id, snapshot_at, snapshot_id, last_offset, dataset_id, \
                  parquet_path, row_count, bytes_on_disk) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    archive.stream_id,
                    cql_ts(archive.snapshot_at),
                    archive.snapshot_id.as_str(),
                    archive.last_offset,
                    archive.dataset_id,
                    archive.parquet_path.as_str(),
                    archive.row_count,
                    archive.bytes_on_disk,
                ),
            )
            .await?;
        Ok(())
    }

    async fn cold_watermark(
        &self,
        stream_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError> {
        let rows = self
            .list_cold_archives(stream_id, DateTime::<Utc>::MIN_UTC, Utc::now(), 1)
            .await?;
        Ok(rows.into_iter().next().map(|item| item.snapshot_at))
    }

    async fn list_cold_archives(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<ColdArchiveRecord>, RuntimeStoreError> {
        let result = self
            .session
            .query(
                "SELECT snapshot_at, snapshot_id, last_offset, dataset_id, \
                 parquet_path, row_count, bytes_on_disk \
                 FROM streaming_runtime.cold_archives_by_stream \
                 WHERE stream_id = ? LIMIT ?",
                (stream_id, limit as i32),
            )
            .await?;
        let mut items = Vec::new();
        for row in result
            .rows_typed_or_empty::<(CqlTimestamp, String, i64, Option<Uuid>, String, i64, i64)>()
        {
            let (
                snapshot_at,
                snapshot_id,
                last_offset,
                dataset_id,
                parquet_path,
                row_count,
                bytes_on_disk,
            ) = row.map_err(|e| {
                RuntimeStoreError::Unavailable(format!("cold archive row decode failed: {e}"))
            })?;
            let snapshot_at = timestamp_to_utc(snapshot_at);
            if snapshot_at < from || snapshot_at > to {
                continue;
            }
            items.push(ColdArchiveRecord {
                stream_id,
                snapshot_id,
                last_offset,
                snapshot_at,
                dataset_id,
                parquet_path,
                row_count,
                bytes_on_disk,
            });
        }
        items.sort_by_key(|item| item.snapshot_at);
        items.truncate(limit);
        Ok(items)
    }
}

#[derive(Debug)]
pub struct HybridRuntimeStore {
    memory: MemoryRuntimeStore,
    cassandra: Option<CassandraRuntimeStore>,
}

impl HybridRuntimeStore {
    pub fn new(cassandra: Option<CassandraRuntimeStore>) -> Self {
        Self {
            memory: MemoryRuntimeStore::new(),
            cassandra,
        }
    }
}

#[async_trait]
impl RuntimeStore for HybridRuntimeStore {
    async fn append_event(
        &self,
        stream_id: Uuid,
        payload: Value,
        event_time: DateTime<Utc>,
    ) -> Result<RuntimeEvent, RuntimeStoreError> {
        self.memory
            .append_event(stream_id, payload, event_time)
            .await
    }

    async fn list_events_since(
        &self,
        stream_id: Uuid,
        after_sequence_no: i64,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        self.memory
            .list_events_since(stream_id, after_sequence_no)
            .await
    }

    async fn list_recent_events(
        &self,
        stream_id: Uuid,
        limit: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        self.memory.list_recent_events(stream_id, limit).await
    }

    async fn list_events_between(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        self.memory
            .list_events_between(stream_id, from, to, limit)
            .await
    }

    async fn live_event_count_since(&self, since: DateTime<Utc>) -> Result<i64, RuntimeStoreError> {
        self.memory.live_event_count_since(since).await
    }

    async fn stream_activity(
        &self,
        recent_since: DateTime<Utc>,
    ) -> Result<HashMap<Uuid, StreamActivity>, RuntimeStoreError> {
        self.memory.stream_activity(recent_since).await
    }

    async fn mark_events_processed(&self, event_ids: &[Uuid]) -> Result<(), RuntimeStoreError> {
        self.memory.mark_events_processed(event_ids).await
    }

    async fn restore_events(
        &self,
        stream_ids: &[Uuid],
        from_sequence_no: Option<i64>,
    ) -> Result<i64, RuntimeStoreError> {
        self.memory
            .restore_events(stream_ids, from_sequence_no)
            .await
    }

    async fn archive_candidates(
        &self,
        stream_id: Uuid,
        retention_cutoff: DateTime<Utc>,
        row_cap: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        self.memory
            .archive_candidates(stream_id, retention_cutoff, row_cap)
            .await
    }

    async fn record_cold_archive(
        &self,
        archive: ColdArchiveRecord,
        archived_event_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError> {
        self.memory
            .record_cold_archive(archive.clone(), archived_event_ids)
            .await?;
        if let Some(cassandra) = &self.cassandra {
            cassandra.insert_cold_archive(&archive).await?;
        }
        Ok(())
    }

    async fn cold_watermark(
        &self,
        stream_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError> {
        if let Some(cassandra) = &self.cassandra {
            return cassandra.cold_watermark(stream_id).await;
        }
        self.memory.cold_watermark(stream_id).await
    }

    async fn list_cold_archives(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<ColdArchiveRecord>, RuntimeStoreError> {
        if let Some(cassandra) = &self.cassandra {
            return cassandra
                .list_cold_archives(stream_id, from, to, limit)
                .await;
        }
        self.memory
            .list_cold_archives(stream_id, from, to, limit)
            .await
    }

    async fn max_sequence_no(&self, stream_id: Uuid) -> Result<i64, RuntimeStoreError> {
        self.memory.max_sequence_no(stream_id).await
    }

    async fn save_topology_offsets(
        &self,
        topology_id: Uuid,
        offsets: HashMap<Uuid, i64>,
    ) -> Result<(), RuntimeStoreError> {
        self.memory
            .save_topology_offsets(topology_id, offsets.clone())
            .await?;
        if let Some(cassandra) = &self.cassandra {
            cassandra.save_offsets(topology_id, &offsets).await?;
        }
        Ok(())
    }

    async fn set_topology_offset(
        &self,
        topology_id: Uuid,
        stream_id: Uuid,
        sequence_no: i64,
    ) -> Result<(), RuntimeStoreError> {
        self.memory
            .set_topology_offset(topology_id, stream_id, sequence_no)
            .await?;
        if let Some(cassandra) = &self.cassandra {
            cassandra
                .set_offset(topology_id, stream_id, sequence_no)
                .await?;
        }
        Ok(())
    }

    async fn clear_topology_offsets(
        &self,
        topology_id: Uuid,
        stream_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError> {
        self.memory
            .clear_topology_offsets(topology_id, stream_ids)
            .await?;
        if let Some(cassandra) = &self.cassandra {
            cassandra.clear_offsets(topology_id, stream_ids).await?;
        }
        Ok(())
    }

    async fn load_topology_offsets(
        &self,
        topology_id: Uuid,
    ) -> Result<HashMap<Uuid, StreamCheckpointOffset>, RuntimeStoreError> {
        if let Some(cassandra) = &self.cassandra {
            return cassandra.load_offsets(topology_id).await;
        }
        self.memory.load_topology_offsets(topology_id).await
    }

    async fn insert_checkpoint(&self, checkpoint: Checkpoint) -> Result<(), RuntimeStoreError> {
        self.memory.insert_checkpoint(checkpoint.clone()).await?;
        if let Some(cassandra) = &self.cassandra {
            cassandra.insert_checkpoint(&checkpoint).await?;
        }
        Ok(())
    }

    async fn load_checkpoint(
        &self,
        topology_id: Uuid,
        checkpoint_id: Option<Uuid>,
    ) -> Result<Option<Checkpoint>, RuntimeStoreError> {
        if let Some(cassandra) = &self.cassandra {
            return cassandra.load_checkpoint(topology_id, checkpoint_id).await;
        }
        self.memory
            .load_checkpoint(topology_id, checkpoint_id)
            .await
    }

    async fn list_checkpoints(
        &self,
        topology_id: Uuid,
        limit: usize,
    ) -> Result<Vec<Checkpoint>, RuntimeStoreError> {
        if let Some(cassandra) = &self.cassandra {
            return cassandra.list_checkpoints(topology_id, limit).await;
        }
        self.memory.list_checkpoints(topology_id, limit).await
    }

    async fn latest_checkpoint_at(
        &self,
        topology_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError> {
        if let Some(cassandra) = &self.cassandra {
            return cassandra.latest_checkpoint_at(topology_id).await;
        }
        self.memory.latest_checkpoint_at(topology_id).await
    }
}

#[async_trait]
impl RuntimeStore for MemoryRuntimeStore {
    async fn append_event(
        &self,
        stream_id: Uuid,
        payload: Value,
        event_time: DateTime<Utc>,
    ) -> Result<RuntimeEvent, RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        let sequence_no = guard
            .next_sequence_by_stream
            .entry(stream_id)
            .and_modify(|value| *value += 1)
            .or_insert(1);
        let event = RuntimeEvent {
            id: Uuid::now_v7(),
            stream_id,
            sequence_no: *sequence_no,
            payload,
            event_time,
            processed_at: None,
            archived_at: None,
            archive_path: None,
        };
        guard
            .events_by_stream
            .entry(stream_id)
            .or_default()
            .push(event.clone());
        Ok(event)
    }

    async fn list_events_since(
        &self,
        stream_id: Uuid,
        after_sequence_no: i64,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .events_by_stream
            .get(&stream_id)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|event| event.sequence_no > after_sequence_no && event.archived_at.is_none())
            .collect())
    }

    async fn list_recent_events(
        &self,
        stream_id: Uuid,
        limit: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        let mut items = guard
            .events_by_stream
            .get(&stream_id)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|event| event.archived_at.is_none())
            .collect::<Vec<_>>();
        let len = items.len();
        if len > limit {
            items = items.split_off(len - limit);
        }
        Ok(items)
    }

    async fn list_events_between(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .events_by_stream
            .get(&stream_id)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|event| {
                event.archived_at.is_none() && event.event_time >= from && event.event_time <= to
            })
            .take(limit)
            .collect())
    }

    async fn live_event_count_since(&self, since: DateTime<Utc>) -> Result<i64, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .events_by_stream
            .values()
            .flat_map(|events| events.iter())
            .filter(|event| event.archived_at.is_none() && event.event_time >= since)
            .count() as i64)
    }

    async fn stream_activity(
        &self,
        recent_since: DateTime<Utc>,
    ) -> Result<HashMap<Uuid, StreamActivity>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        let mut stats = HashMap::new();
        for (stream_id, events) in &guard.events_by_stream {
            let mut item = StreamActivity::default();
            for event in events {
                if event.archived_at.is_none() && event.processed_at.is_none() {
                    item.backlog += 1;
                }
                if event.archived_at.is_none() && event.event_time >= recent_since {
                    item.recent_events += 1;
                    item.oldest_event_time = Some(
                        item.oldest_event_time
                            .map_or(event.event_time, |ts| ts.min(event.event_time)),
                    );
                    item.newest_event_time = Some(
                        item.newest_event_time
                            .map_or(event.event_time, |ts| ts.max(event.event_time)),
                    );
                }
            }
            stats.insert(*stream_id, item);
        }
        Ok(stats)
    }

    async fn mark_events_processed(&self, event_ids: &[Uuid]) -> Result<(), RuntimeStoreError> {
        let now = Utc::now();
        let mut guard = self.inner.write().await;
        for events in guard.events_by_stream.values_mut() {
            for event in events.iter_mut() {
                if event_ids.contains(&event.id) {
                    event.processed_at = Some(now);
                }
            }
        }
        Ok(())
    }

    async fn restore_events(
        &self,
        stream_ids: &[Uuid],
        from_sequence_no: Option<i64>,
    ) -> Result<i64, RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        let mut restored = 0i64;
        for stream_id in stream_ids {
            if let Some(events) = guard.events_by_stream.get_mut(stream_id) {
                for event in events.iter_mut() {
                    let matches = from_sequence_no
                        .map(|sequence_no| event.sequence_no >= sequence_no)
                        .unwrap_or(true);
                    if matches {
                        event.processed_at = None;
                        event.archived_at = None;
                        event.archive_path = None;
                        restored += 1;
                    }
                }
            }
        }
        Ok(restored)
    }

    async fn archive_candidates(
        &self,
        stream_id: Uuid,
        retention_cutoff: DateTime<Utc>,
        row_cap: usize,
    ) -> Result<Vec<RuntimeEvent>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .events_by_stream
            .get(&stream_id)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|event| {
                event.archived_at.is_none()
                    && event.processed_at.is_some()
                    && event.event_time <= retention_cutoff
            })
            .take(row_cap)
            .collect())
    }

    async fn record_cold_archive(
        &self,
        archive: ColdArchiveRecord,
        archived_event_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        let snapshot_at = archive.snapshot_at;
        let parquet_path = archive.parquet_path.clone();
        for events in guard.events_by_stream.values_mut() {
            for event in events.iter_mut() {
                if archived_event_ids.contains(&event.id) {
                    event.archived_at = Some(snapshot_at);
                    event.archive_path = Some(parquet_path.clone());
                }
            }
        }
        guard
            .cold_archives
            .entry(archive.stream_id)
            .or_default()
            .push(archive);
        Ok(())
    }

    async fn cold_watermark(
        &self,
        stream_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .cold_archives
            .get(&stream_id)
            .and_then(|items| items.iter().map(|item| item.snapshot_at).max()))
    }

    async fn list_cold_archives(
        &self,
        stream_id: Uuid,
        from: DateTime<Utc>,
        to: DateTime<Utc>,
        limit: usize,
    ) -> Result<Vec<ColdArchiveRecord>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        let mut items = guard
            .cold_archives
            .get(&stream_id)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|item| item.snapshot_at >= from && item.snapshot_at <= to)
            .collect::<Vec<_>>();
        items.sort_by_key(|item| item.snapshot_at);
        items.truncate(limit);
        Ok(items)
    }

    async fn max_sequence_no(&self, stream_id: Uuid) -> Result<i64, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .events_by_stream
            .get(&stream_id)
            .and_then(|items| items.last())
            .map(|event| event.sequence_no)
            .unwrap_or(0))
    }

    async fn save_topology_offsets(
        &self,
        topology_id: Uuid,
        offsets: HashMap<Uuid, i64>,
    ) -> Result<(), RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        let updated_at = Utc::now();
        let bucket = guard.topology_offsets.entry(topology_id).or_default();
        for (stream_id, last_sequence_no) in offsets {
            bucket.insert(
                stream_id,
                StreamCheckpointOffset {
                    stream_id,
                    last_sequence_no,
                    updated_at,
                },
            );
        }
        Ok(())
    }

    async fn set_topology_offset(
        &self,
        topology_id: Uuid,
        stream_id: Uuid,
        sequence_no: i64,
    ) -> Result<(), RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        guard
            .topology_offsets
            .entry(topology_id)
            .or_default()
            .insert(
                stream_id,
                StreamCheckpointOffset {
                    stream_id,
                    last_sequence_no: sequence_no,
                    updated_at: Utc::now(),
                },
            );
        Ok(())
    }

    async fn clear_topology_offsets(
        &self,
        topology_id: Uuid,
        stream_ids: &[Uuid],
    ) -> Result<(), RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        if let Some(items) = guard.topology_offsets.get_mut(&topology_id) {
            for stream_id in stream_ids {
                items.remove(stream_id);
            }
        }
        Ok(())
    }

    async fn load_topology_offsets(
        &self,
        topology_id: Uuid,
    ) -> Result<HashMap<Uuid, StreamCheckpointOffset>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .topology_offsets
            .get(&topology_id)
            .cloned()
            .unwrap_or_default())
    }

    async fn insert_checkpoint(&self, checkpoint: Checkpoint) -> Result<(), RuntimeStoreError> {
        let mut guard = self.inner.write().await;
        guard
            .checkpoints
            .entry(checkpoint.topology_id)
            .or_default()
            .push(checkpoint);
        Ok(())
    }

    async fn load_checkpoint(
        &self,
        topology_id: Uuid,
        checkpoint_id: Option<Uuid>,
    ) -> Result<Option<Checkpoint>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        let items = guard
            .checkpoints
            .get(&topology_id)
            .cloned()
            .unwrap_or_default();
        Ok(match checkpoint_id {
            Some(id) => items.into_iter().find(|item| item.id == id),
            None => items.into_iter().max_by_key(|item| item.created_at),
        })
    }

    async fn list_checkpoints(
        &self,
        topology_id: Uuid,
        limit: usize,
    ) -> Result<Vec<Checkpoint>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        let mut items = guard
            .checkpoints
            .get(&topology_id)
            .cloned()
            .unwrap_or_default();
        items.sort_by_key(|item| std::cmp::Reverse(item.created_at));
        items.truncate(limit);
        Ok(items)
    }

    async fn latest_checkpoint_at(
        &self,
        topology_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RuntimeStoreError> {
        let guard = self.inner.read().await;
        Ok(guard
            .checkpoints
            .get(&topology_id)
            .and_then(|items| items.iter().map(|item| item.created_at).max()))
    }
}

fn cql_ts(value: DateTime<Utc>) -> CqlTimestamp {
    CqlTimestamp(value.timestamp_millis())
}

fn timestamp_to_utc(value: CqlTimestamp) -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp_millis(value.0).unwrap_or(DateTime::<Utc>::UNIX_EPOCH)
}

fn build_checkpoint(
    id: Uuid,
    topology_id: Uuid,
    status: &str,
    last_offsets: &str,
    state_uri: Option<String>,
    savepoint_uri: Option<String>,
    trigger: &str,
    duration_ms: i32,
    created_at: DateTime<Utc>,
) -> Result<Checkpoint, RuntimeStoreError> {
    Ok(Checkpoint {
        id,
        topology_id,
        status: status.to_string(),
        last_offsets: serde_json::from_str(last_offsets)
            .map_err(|e| RuntimeStoreError::Serialize(e.to_string()))?,
        state_uri,
        savepoint_uri,
        trigger: trigger.to_string(),
        duration_ms,
        created_at,
    })
}
