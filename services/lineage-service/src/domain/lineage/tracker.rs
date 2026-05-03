use std::collections::{BTreeMap, HashMap};
use std::sync::Arc;
use std::time::Duration;

use ::cassandra_kernel::scylla::{Session, frame::value::CqlTimestamp};
use ::cassandra_kernel::{ClusterConfig, SessionBuilder};
use chrono::{DateTime, Utc};
use serde_json::Value;
use tokio::sync::RwLock;
use tracing::warn;
use uuid::Uuid;

use super::{
    ColumnLineageEdge, LineageRelationRecord, METADATA_SOURCE_COLUMN, METADATA_TARGET_COLUMN,
    NodeKey,
};

const KEYSPACE: &str = "lineage_runtime";
const RUNTIME_PARTITION: &str = "runtime";

const KEYSPACE_DDL: &str = "\
CREATE KEYSPACE IF NOT EXISTS lineage_runtime \
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}";

const RELATIONS_BY_SOURCE_DDL: &str = "\
CREATE TABLE IF NOT EXISTS lineage_runtime.relations_by_source ( \
    source_kind         text, \
    source_id           uuid, \
    relation_id         uuid, \
    source_kind_copy    text, \
    target_id           uuid, \
    target_kind         text, \
    relation_kind       text, \
    pipeline_id         uuid, \
    workflow_id         uuid, \
    node_id             text, \
    step_id             text, \
    effective_marking   text, \
    metadata            text, \
    created_at          timestamp, \
    PRIMARY KEY ((source_kind, source_id), relation_id) \
)";

const RELATIONS_BY_TARGET_DDL: &str = "\
CREATE TABLE IF NOT EXISTS lineage_runtime.relations_by_target ( \
    target_kind         text, \
    target_id           uuid, \
    relation_id         uuid, \
    source_id           uuid, \
    source_kind         text, \
    target_kind_copy    text, \
    relation_kind       text, \
    pipeline_id         uuid, \
    workflow_id         uuid, \
    node_id             text, \
    step_id             text, \
    effective_marking   text, \
    metadata            text, \
    created_at          timestamp, \
    PRIMARY KEY ((target_kind, target_id), relation_id) \
)";

const RELATIONS_ALL_DDL: &str = "\
CREATE TABLE IF NOT EXISTS lineage_runtime.relations_all ( \
    partition_key       text, \
    relation_id         uuid, \
    source_id           uuid, \
    source_kind         text, \
    target_id           uuid, \
    target_kind         text, \
    relation_kind       text, \
    pipeline_id         uuid, \
    workflow_id         uuid, \
    node_id             text, \
    step_id             text, \
    effective_marking   text, \
    metadata            text, \
    created_at          timestamp, \
    PRIMARY KEY ((partition_key), relation_id) \
)";

const RELATIONS_BY_WORKFLOW_DDL: &str = "\
CREATE TABLE IF NOT EXISTS lineage_runtime.relations_by_workflow ( \
    workflow_id         uuid, \
    relation_id         uuid, \
    source_id           uuid, \
    source_kind         text, \
    target_id           uuid, \
    target_kind         text, \
    relation_kind       text, \
    pipeline_id         uuid, \
    node_id             text, \
    step_id             text, \
    effective_marking   text, \
    metadata            text, \
    created_at          timestamp, \
    PRIMARY KEY ((workflow_id), relation_id) \
)";

const COLUMN_RELATIONS_BY_DATASET_DDL: &str = "\
CREATE TABLE IF NOT EXISTS lineage_runtime.column_relations_by_dataset ( \
    dataset_id          uuid, \
    relation_id         uuid, \
    source_dataset_id   uuid, \
    source_column       text, \
    target_dataset_id   uuid, \
    target_column       text, \
    pipeline_id         uuid, \
    node_id             text, \
    created_at          timestamp, \
    PRIMARY KEY ((dataset_id), relation_id) \
)";

#[derive(Debug, thiserror::Error)]
pub enum LineageRuntimeStoreError {
    #[error("lineage runtime store unavailable: {0}")]
    Unavailable(String),
    #[error("lineage runtime store serialisation error: {0}")]
    Serialize(String),
    #[error("cassandra runtime store error: {0}")]
    Cassandra(#[from] ::cassandra_kernel::KernelError),
    #[error("cassandra query error: {0}")]
    Query(#[from] ::cassandra_kernel::scylla::transport::errors::QueryError),
}

#[derive(Debug, Clone)]
pub struct LineageRuntimeStoreConfig {
    pub cassandra_contact_points: Vec<String>,
    pub cassandra_local_dc: String,
}

impl Default for LineageRuntimeStoreConfig {
    fn default() -> Self {
        Self {
            cassandra_contact_points: Vec::new(),
            cassandra_local_dc: "dc1".to_string(),
        }
    }
}

pub type SharedLineageRuntimeStore = Arc<LineageRuntimeStore>;

#[derive(Debug)]
pub enum LineageRuntimeStore {
    Memory(MemoryLineageRuntimeStore),
    Cassandra(CassandraLineageRuntimeStore),
}

impl LineageRuntimeStore {
    pub async fn build(
        config: LineageRuntimeStoreConfig,
    ) -> Result<SharedLineageRuntimeStore, LineageRuntimeStoreError> {
        if config.cassandra_contact_points.is_empty() {
            warn!(
                "CASSANDRA_CONTACT_POINTS not set for lineage runtime; falling back to in-memory lineage store"
            );
            return Ok(Arc::new(Self::Memory(MemoryLineageRuntimeStore::new())));
        }

        let cluster = ClusterConfig {
            contact_points: config.cassandra_contact_points,
            local_datacenter: config.cassandra_local_dc,
            keyspace: Some(KEYSPACE.to_string()),
            connect_timeout: Duration::from_secs(5),
            request_timeout: Duration::from_secs(5),
            ..ClusterConfig::dev_local()
        };
        let session = Arc::new(SessionBuilder::new(cluster).build().await?);
        let store = CassandraLineageRuntimeStore::new(session);
        store.migrate().await?;
        Ok(Arc::new(Self::Cassandra(store)))
    }

    pub(super) async fn record_relation(
        &self,
        relation: &LineageRelationRecord,
    ) -> Result<(), LineageRuntimeStoreError> {
        match self {
            Self::Memory(store) => store.record_relation(relation).await,
            Self::Cassandra(store) => store.record_relation(relation).await,
        }
    }

    pub(super) async fn adjacent_relations(
        &self,
        node: NodeKey,
    ) -> Result<Vec<LineageRelationRecord>, LineageRuntimeStoreError> {
        match self {
            Self::Memory(store) => store.adjacent_relations(node).await,
            Self::Cassandra(store) => store.adjacent_relations(node).await,
        }
    }

    pub(super) async fn all_relations(
        &self,
    ) -> Result<Vec<LineageRelationRecord>, LineageRuntimeStoreError> {
        match self {
            Self::Memory(store) => store.all_relations().await,
            Self::Cassandra(store) => store.all_relations().await,
        }
    }

    pub(super) async fn delete_workflow_relations(
        &self,
        workflow_id: Uuid,
    ) -> Result<(), LineageRuntimeStoreError> {
        match self {
            Self::Memory(store) => store.delete_workflow_relations(workflow_id).await,
            Self::Cassandra(store) => store.delete_workflow_relations(workflow_id).await,
        }
    }

    pub(super) async fn dataset_column_lineage(
        &self,
        dataset_id: Uuid,
    ) -> Result<Vec<ColumnLineageEdge>, LineageRuntimeStoreError> {
        match self {
            Self::Memory(store) => store.dataset_column_lineage(dataset_id).await,
            Self::Cassandra(store) => store.dataset_column_lineage(dataset_id).await,
        }
    }
}

#[derive(Debug, Default)]
struct MemoryLineageRuntimeState {
    relations: HashMap<Uuid, LineageRelationRecord>,
}

#[derive(Debug, Default)]
pub struct MemoryLineageRuntimeStore {
    inner: RwLock<MemoryLineageRuntimeState>,
}

impl MemoryLineageRuntimeStore {
    pub fn new() -> Self {
        Self::default()
    }

    async fn record_relation(
        &self,
        relation: &LineageRelationRecord,
    ) -> Result<(), LineageRuntimeStoreError> {
        self.inner
            .write()
            .await
            .relations
            .insert(relation.id, relation.clone());
        Ok(())
    }

    async fn adjacent_relations(
        &self,
        node: NodeKey,
    ) -> Result<Vec<LineageRelationRecord>, LineageRuntimeStoreError> {
        let state = self.inner.read().await;
        Ok(state
            .relations
            .values()
            .filter(|relation| {
                (relation.source_id == node.id && relation.source_kind == node.kind.as_str())
                    || (relation.target_id == node.id && relation.target_kind == node.kind.as_str())
            })
            .cloned()
            .collect())
    }

    async fn all_relations(&self) -> Result<Vec<LineageRelationRecord>, LineageRuntimeStoreError> {
        Ok(self
            .inner
            .read()
            .await
            .relations
            .values()
            .cloned()
            .collect())
    }

    async fn delete_workflow_relations(
        &self,
        workflow_id: Uuid,
    ) -> Result<(), LineageRuntimeStoreError> {
        self.inner
            .write()
            .await
            .relations
            .retain(|_, relation| relation.workflow_id != Some(workflow_id));
        Ok(())
    }

    async fn dataset_column_lineage(
        &self,
        dataset_id: Uuid,
    ) -> Result<Vec<ColumnLineageEdge>, LineageRuntimeStoreError> {
        let mut edges = self
            .inner
            .read()
            .await
            .relations
            .values()
            .filter(|relation| relation.source_id == dataset_id || relation.target_id == dataset_id)
            .filter_map(|relation| column_edge_from_relation(relation).ok())
            .collect::<Vec<_>>();
        edges.sort_by_key(|edge| std::cmp::Reverse(edge.created_at));
        Ok(edges)
    }
}

#[derive(Debug, Clone)]
pub struct CassandraLineageRuntimeStore {
    session: Arc<Session>,
}

impl CassandraLineageRuntimeStore {
    pub fn new(session: Arc<Session>) -> Self {
        Self { session }
    }

    pub async fn migrate(&self) -> Result<(), LineageRuntimeStoreError> {
        self.session.query(KEYSPACE_DDL, ()).await?;
        self.session.query(RELATIONS_BY_SOURCE_DDL, ()).await?;
        self.session.query(RELATIONS_BY_TARGET_DDL, ()).await?;
        self.session.query(RELATIONS_ALL_DDL, ()).await?;
        self.session.query(RELATIONS_BY_WORKFLOW_DDL, ()).await?;
        self.session
            .query(COLUMN_RELATIONS_BY_DATASET_DDL, ())
            .await?;
        Ok(())
    }

    async fn record_relation(
        &self,
        relation: &LineageRelationRecord,
    ) -> Result<(), LineageRuntimeStoreError> {
        let metadata = serde_json::to_string(&relation.metadata)
            .map_err(|error| LineageRuntimeStoreError::Serialize(error.to_string()))?;
        let created_at = cql_ts(relation.created_at);

        self.session
            .query(
                "INSERT INTO lineage_runtime.relations_by_source \
                 (source_kind, source_id, relation_id, source_kind_copy, target_id, target_kind, \
                  relation_kind, pipeline_id, workflow_id, node_id, step_id, effective_marking, \
                  metadata, created_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    relation.source_kind.as_str(),
                    relation.source_id,
                    relation.id,
                    relation.source_kind.as_str(),
                    relation.target_id,
                    relation.target_kind.as_str(),
                    relation.relation_kind.as_str(),
                    relation.pipeline_id,
                    relation.workflow_id,
                    relation.node_id.as_deref(),
                    relation.step_id.as_deref(),
                    relation.effective_marking.as_str(),
                    metadata.as_str(),
                    created_at,
                ),
            )
            .await?;

        self.session
            .query(
                "INSERT INTO lineage_runtime.relations_by_target \
                 (target_kind, target_id, relation_id, source_id, source_kind, target_kind_copy, \
                  relation_kind, pipeline_id, workflow_id, node_id, step_id, effective_marking, \
                  metadata, created_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    relation.target_kind.as_str(),
                    relation.target_id,
                    relation.id,
                    relation.source_id,
                    relation.source_kind.as_str(),
                    relation.target_kind.as_str(),
                    relation.relation_kind.as_str(),
                    relation.pipeline_id,
                    relation.workflow_id,
                    relation.node_id.as_deref(),
                    relation.step_id.as_deref(),
                    relation.effective_marking.as_str(),
                    metadata.as_str(),
                    created_at,
                ),
            )
            .await?;

        self.session
            .query(
                "INSERT INTO lineage_runtime.relations_all \
                 (partition_key, relation_id, source_id, source_kind, target_id, target_kind, \
                  relation_kind, pipeline_id, workflow_id, node_id, step_id, effective_marking, \
                  metadata, created_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    RUNTIME_PARTITION,
                    relation.id,
                    relation.source_id,
                    relation.source_kind.as_str(),
                    relation.target_id,
                    relation.target_kind.as_str(),
                    relation.relation_kind.as_str(),
                    relation.pipeline_id,
                    relation.workflow_id,
                    relation.node_id.as_deref(),
                    relation.step_id.as_deref(),
                    relation.effective_marking.as_str(),
                    metadata.as_str(),
                    created_at,
                ),
            )
            .await?;

        if let Some(workflow_id) = relation.workflow_id {
            self.session
                .query(
                    "INSERT INTO lineage_runtime.relations_by_workflow \
                     (workflow_id, relation_id, source_id, source_kind, target_id, target_kind, \
                      relation_kind, pipeline_id, node_id, step_id, effective_marking, metadata, \
                      created_at) \
                     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                    (
                        workflow_id,
                        relation.id,
                        relation.source_id,
                        relation.source_kind.as_str(),
                        relation.target_id,
                        relation.target_kind.as_str(),
                        relation.relation_kind.as_str(),
                        relation.pipeline_id,
                        relation.node_id.as_deref(),
                        relation.step_id.as_deref(),
                        relation.effective_marking.as_str(),
                        metadata.as_str(),
                        created_at,
                    ),
                )
                .await?;
        }

        if let Ok(column_edge) = column_edge_from_relation(relation) {
            for dataset_id in [column_edge.source_dataset_id, column_edge.target_dataset_id] {
                self.session
                    .query(
                        "INSERT INTO lineage_runtime.column_relations_by_dataset \
                         (dataset_id, relation_id, source_dataset_id, source_column, \
                          target_dataset_id, target_column, pipeline_id, node_id, created_at) \
                         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                        (
                            dataset_id,
                            column_edge.id,
                            column_edge.source_dataset_id,
                            column_edge.source_column.as_str(),
                            column_edge.target_dataset_id,
                            column_edge.target_column.as_str(),
                            column_edge.pipeline_id,
                            column_edge.node_id.as_deref(),
                            created_at,
                        ),
                    )
                    .await?;
            }
        }

        Ok(())
    }

    async fn adjacent_relations(
        &self,
        node: NodeKey,
    ) -> Result<Vec<LineageRelationRecord>, LineageRuntimeStoreError> {
        let mut merged = BTreeMap::new();

        let source_rows = self
            .session
            .query(
                "SELECT relation_id, source_id, source_kind_copy, target_id, target_kind, \
                        relation_kind, pipeline_id, workflow_id, node_id, step_id, \
                        effective_marking, metadata, created_at \
                 FROM lineage_runtime.relations_by_source \
                 WHERE source_kind = ? AND source_id = ?",
                (node.kind.as_str(), node.id),
            )
            .await?;
        for row in source_rows.rows_typed_or_empty::<(
            Uuid,
            Uuid,
            String,
            Uuid,
            String,
            String,
            Option<Uuid>,
            Option<Uuid>,
            Option<String>,
            Option<String>,
            String,
            String,
            CqlTimestamp,
        )>() {
            let row = row.map_err(|error| {
                LineageRuntimeStoreError::Unavailable(format!(
                    "failed to decode source lineage row: {error}"
                ))
            })?;
            let relation = relation_from_source_row(row)?;
            merged.insert(relation.id, relation);
        }

        let target_rows = self
            .session
            .query(
                "SELECT relation_id, source_id, source_kind, target_id, target_kind_copy, \
                        relation_kind, pipeline_id, workflow_id, node_id, step_id, \
                        effective_marking, metadata, created_at \
                 FROM lineage_runtime.relations_by_target \
                 WHERE target_kind = ? AND target_id = ?",
                (node.kind.as_str(), node.id),
            )
            .await?;
        for row in target_rows.rows_typed_or_empty::<(
            Uuid,
            Uuid,
            String,
            Uuid,
            String,
            String,
            Option<Uuid>,
            Option<Uuid>,
            Option<String>,
            Option<String>,
            String,
            String,
            CqlTimestamp,
        )>() {
            let row = row.map_err(|error| {
                LineageRuntimeStoreError::Unavailable(format!(
                    "failed to decode target lineage row: {error}"
                ))
            })?;
            let relation = relation_from_target_row(row)?;
            merged.insert(relation.id, relation);
        }

        Ok(merged.into_values().collect())
    }

    async fn all_relations(&self) -> Result<Vec<LineageRelationRecord>, LineageRuntimeStoreError> {
        let rows = self
            .session
            .query(
                "SELECT relation_id, source_id, source_kind, target_id, target_kind, relation_kind, \
                        pipeline_id, workflow_id, node_id, step_id, effective_marking, metadata, \
                        created_at \
                 FROM lineage_runtime.relations_all \
                 WHERE partition_key = ?",
                (RUNTIME_PARTITION,),
            )
            .await?;

        let mut relations = Vec::new();
        for row in rows.rows_typed_or_empty::<(
            Uuid,
            Uuid,
            String,
            Uuid,
            String,
            String,
            Option<Uuid>,
            Option<Uuid>,
            Option<String>,
            Option<String>,
            String,
            String,
            CqlTimestamp,
        )>() {
            let row = row.map_err(|error| {
                LineageRuntimeStoreError::Unavailable(format!(
                    "failed to decode full lineage row: {error}"
                ))
            })?;
            relations.push(relation_from_all_row(row)?);
        }
        Ok(relations)
    }

    async fn delete_workflow_relations(
        &self,
        workflow_id: Uuid,
    ) -> Result<(), LineageRuntimeStoreError> {
        let rows = self
            .session
            .query(
                "SELECT relation_id, source_id, source_kind, target_id, target_kind, relation_kind, \
                        pipeline_id, node_id, step_id, effective_marking, metadata, created_at \
                 FROM lineage_runtime.relations_by_workflow \
                 WHERE workflow_id = ?",
                (workflow_id,),
            )
            .await?;

        let mut relations = Vec::new();
        for row in rows.rows_typed_or_empty::<(
            Uuid,
            Uuid,
            String,
            Uuid,
            String,
            String,
            Option<Uuid>,
            Option<String>,
            Option<String>,
            String,
            String,
            CqlTimestamp,
        )>() {
            let row = row.map_err(|error| {
                LineageRuntimeStoreError::Unavailable(format!(
                    "failed to decode workflow lineage row: {error}"
                ))
            })?;
            relations.push(relation_from_workflow_row(workflow_id, row)?);
        }

        for relation in relations {
            self.session
                .query(
                    "DELETE FROM lineage_runtime.relations_by_source \
                     WHERE source_kind = ? AND source_id = ? AND relation_id = ?",
                    (
                        relation.source_kind.as_str(),
                        relation.source_id,
                        relation.id,
                    ),
                )
                .await?;
            self.session
                .query(
                    "DELETE FROM lineage_runtime.relations_by_target \
                     WHERE target_kind = ? AND target_id = ? AND relation_id = ?",
                    (
                        relation.target_kind.as_str(),
                        relation.target_id,
                        relation.id,
                    ),
                )
                .await?;
            self.session
                .query(
                    "DELETE FROM lineage_runtime.relations_all \
                     WHERE partition_key = ? AND relation_id = ?",
                    (RUNTIME_PARTITION, relation.id),
                )
                .await?;
            self.session
                .query(
                    "DELETE FROM lineage_runtime.relations_by_workflow \
                     WHERE workflow_id = ? AND relation_id = ?",
                    (workflow_id, relation.id),
                )
                .await?;
        }

        Ok(())
    }

    async fn dataset_column_lineage(
        &self,
        dataset_id: Uuid,
    ) -> Result<Vec<ColumnLineageEdge>, LineageRuntimeStoreError> {
        let rows = self
            .session
            .query(
                "SELECT relation_id, source_dataset_id, source_column, target_dataset_id, \
                        target_column, pipeline_id, node_id, created_at \
                 FROM lineage_runtime.column_relations_by_dataset \
                 WHERE dataset_id = ?",
                (dataset_id,),
            )
            .await?;

        let mut edges = BTreeMap::new();
        for row in rows.rows_typed_or_empty::<(
            Uuid,
            Uuid,
            String,
            Uuid,
            String,
            Option<Uuid>,
            Option<String>,
            CqlTimestamp,
        )>() {
            let row = row.map_err(|error| {
                LineageRuntimeStoreError::Unavailable(format!(
                    "failed to decode column lineage row: {error}"
                ))
            })?;
            let edge = column_edge_from_row(row);
            edges.insert(edge.id, edge);
        }

        let mut edges: Vec<_> = edges.into_values().collect();
        edges.sort_by_key(|edge| std::cmp::Reverse(edge.created_at));
        Ok(edges)
    }
}

fn relation_from_source_row(
    row: (
        Uuid,
        Uuid,
        String,
        Uuid,
        String,
        String,
        Option<Uuid>,
        Option<Uuid>,
        Option<String>,
        Option<String>,
        String,
        String,
        CqlTimestamp,
    ),
) -> Result<LineageRelationRecord, LineageRuntimeStoreError> {
    let (
        id,
        source_id,
        source_kind,
        target_id,
        target_kind,
        relation_kind,
        pipeline_id,
        workflow_id,
        node_id,
        step_id,
        effective_marking,
        metadata,
        created_at,
    ) = row;
    Ok(LineageRelationRecord {
        id,
        source_id,
        source_kind,
        target_id,
        target_kind,
        relation_kind,
        pipeline_id,
        workflow_id,
        node_id,
        step_id,
        effective_marking,
        metadata: parse_metadata(&metadata)?,
        created_at: timestamp_to_utc(created_at),
    })
}

fn relation_from_target_row(
    row: (
        Uuid,
        Uuid,
        String,
        Uuid,
        String,
        String,
        Option<Uuid>,
        Option<Uuid>,
        Option<String>,
        Option<String>,
        String,
        String,
        CqlTimestamp,
    ),
) -> Result<LineageRelationRecord, LineageRuntimeStoreError> {
    let (
        id,
        source_id,
        source_kind,
        target_id,
        target_kind,
        relation_kind,
        pipeline_id,
        workflow_id,
        node_id,
        step_id,
        effective_marking,
        metadata,
        created_at,
    ) = row;
    Ok(LineageRelationRecord {
        id,
        source_id,
        source_kind,
        target_id,
        target_kind,
        relation_kind,
        pipeline_id,
        workflow_id,
        node_id,
        step_id,
        effective_marking,
        metadata: parse_metadata(&metadata)?,
        created_at: timestamp_to_utc(created_at),
    })
}

fn relation_from_all_row(
    row: (
        Uuid,
        Uuid,
        String,
        Uuid,
        String,
        String,
        Option<Uuid>,
        Option<Uuid>,
        Option<String>,
        Option<String>,
        String,
        String,
        CqlTimestamp,
    ),
) -> Result<LineageRelationRecord, LineageRuntimeStoreError> {
    relation_from_target_row(row)
}

fn relation_from_workflow_row(
    workflow_id: Uuid,
    row: (
        Uuid,
        Uuid,
        String,
        Uuid,
        String,
        String,
        Option<Uuid>,
        Option<String>,
        Option<String>,
        String,
        String,
        CqlTimestamp,
    ),
) -> Result<LineageRelationRecord, LineageRuntimeStoreError> {
    let (
        id,
        source_id,
        source_kind,
        target_id,
        target_kind,
        relation_kind,
        pipeline_id,
        node_id,
        step_id,
        effective_marking,
        metadata,
        created_at,
    ) = row;
    Ok(LineageRelationRecord {
        id,
        source_id,
        source_kind,
        target_id,
        target_kind,
        relation_kind,
        pipeline_id,
        workflow_id: Some(workflow_id),
        node_id,
        step_id,
        effective_marking,
        metadata: parse_metadata(&metadata)?,
        created_at: timestamp_to_utc(created_at),
    })
}

fn column_edge_from_relation(
    relation: &LineageRelationRecord,
) -> Result<ColumnLineageEdge, LineageRuntimeStoreError> {
    let source_column = relation
        .metadata
        .get(METADATA_SOURCE_COLUMN)
        .and_then(Value::as_str)
        .ok_or_else(|| {
            LineageRuntimeStoreError::Unavailable(
                "column lineage relation missing source_column".to_string(),
            )
        })?;
    let target_column = relation
        .metadata
        .get(METADATA_TARGET_COLUMN)
        .and_then(Value::as_str)
        .ok_or_else(|| {
            LineageRuntimeStoreError::Unavailable(
                "column lineage relation missing target_column".to_string(),
            )
        })?;

    Ok(ColumnLineageEdge {
        id: relation.id,
        source_dataset_id: relation.source_id,
        source_column: source_column.to_string(),
        target_dataset_id: relation.target_id,
        target_column: target_column.to_string(),
        pipeline_id: relation.pipeline_id,
        node_id: relation.node_id.clone(),
        created_at: relation.created_at,
    })
}

fn column_edge_from_row(
    row: (
        Uuid,
        Uuid,
        String,
        Uuid,
        String,
        Option<Uuid>,
        Option<String>,
        CqlTimestamp,
    ),
) -> ColumnLineageEdge {
    let (
        id,
        source_dataset_id,
        source_column,
        target_dataset_id,
        target_column,
        pipeline_id,
        node_id,
        created_at,
    ) = row;
    ColumnLineageEdge {
        id,
        source_dataset_id,
        source_column,
        target_dataset_id,
        target_column,
        pipeline_id,
        node_id,
        created_at: timestamp_to_utc(created_at),
    }
}

fn parse_metadata(raw: &str) -> Result<Value, LineageRuntimeStoreError> {
    serde_json::from_str(raw).map_err(|error| {
        LineageRuntimeStoreError::Serialize(format!("invalid lineage metadata JSON: {error}"))
    })
}

fn cql_ts(value: DateTime<Utc>) -> CqlTimestamp {
    CqlTimestamp(value.timestamp_millis())
}

fn timestamp_to_utc(value: CqlTimestamp) -> DateTime<Utc> {
    DateTime::from_timestamp_millis(value.0).unwrap_or_else(Utc::now)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn memory_store_round_trips_adjacent_relations() {
        let store = MemoryLineageRuntimeStore::new();
        let dataset_id = Uuid::now_v7();
        let pipeline_id = Uuid::now_v7();
        let relation = LineageRelationRecord {
            id: Uuid::now_v7(),
            source_id: dataset_id,
            source_kind: "dataset".to_string(),
            target_id: pipeline_id,
            target_kind: "pipeline".to_string(),
            relation_kind: "consumes".to_string(),
            pipeline_id: Some(pipeline_id),
            workflow_id: None,
            node_id: Some("node-a".to_string()),
            step_id: None,
            effective_marking: "public".to_string(),
            metadata: serde_json::json!({}),
            created_at: Utc::now(),
        };

        store
            .record_relation(&relation)
            .await
            .expect("store relation");
        let adjacent = store
            .adjacent_relations(NodeKey {
                id: dataset_id,
                kind: super::super::NodeKind::Dataset,
            })
            .await
            .expect("query adjacent");

        assert_eq!(adjacent.len(), 1);
        assert_eq!(adjacent[0].id, relation.id);
    }

    #[tokio::test]
    async fn memory_store_deduplicates_column_lineage_by_relation() {
        let store = MemoryLineageRuntimeStore::new();
        let source_id = Uuid::now_v7();
        let target_id = Uuid::now_v7();
        let relation = LineageRelationRecord {
            id: Uuid::now_v7(),
            source_id,
            source_kind: "dataset".to_string(),
            target_id,
            target_kind: "dataset".to_string(),
            relation_kind: "column_derives".to_string(),
            pipeline_id: None,
            workflow_id: None,
            node_id: None,
            step_id: None,
            effective_marking: "public".to_string(),
            metadata: serde_json::json!({
                METADATA_SOURCE_COLUMN: "a",
                METADATA_TARGET_COLUMN: "b",
            }),
            created_at: Utc::now(),
        };

        store
            .record_relation(&relation)
            .await
            .expect("store relation");
        let edges = store
            .dataset_column_lineage(source_id)
            .await
            .expect("column lineage");

        assert_eq!(edges.len(), 1);
        assert_eq!(edges[0].source_column, "a");
        assert_eq!(edges[0].target_column, "b");
    }
}
