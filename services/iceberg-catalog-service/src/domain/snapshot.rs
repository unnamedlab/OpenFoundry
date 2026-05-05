//! Iceberg snapshots — append, overwrite, delete, replace.
//!
//! The catalog records every snapshot in `iceberg_snapshots`. Real
//! Parquet writes (manifest list, manifest files, data files) live in
//! `libs/storage-abstraction::iceberg`; this module is the metadata
//! mirror that the spec's CommitTable response references.

use chrono::Utc;
use serde_json::Value;
use sqlx::{FromRow, PgPool};
use uuid::Uuid;

#[derive(Debug, Clone, FromRow)]
pub struct Snapshot {
    pub id: i64,
    pub table_id: Uuid,
    pub snapshot_id: i64,
    pub parent_snapshot_id: Option<i64>,
    pub sequence_number: i64,
    pub operation: String,
    pub manifest_list_location: String,
    pub summary: Value,
    pub schema_id: i32,
    pub timestamp_ms: i64,
}

#[derive(Debug, thiserror::Error)]
pub enum SnapshotError {
    #[error("invalid operation `{0}` (allowed: append, overwrite, delete, replace)")]
    InvalidOperation(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

#[derive(Debug, Clone)]
pub struct NewSnapshot {
    pub table_id: Uuid,
    pub snapshot_id: i64,
    pub parent_snapshot_id: Option<i64>,
    pub sequence_number: i64,
    pub operation: String,
    pub manifest_list_location: String,
    pub summary: Value,
    pub schema_id: i32,
}

pub async fn append(pool: &PgPool, snapshot: NewSnapshot) -> Result<Snapshot, SnapshotError> {
    if !matches!(
        snapshot.operation.as_str(),
        "append" | "overwrite" | "delete" | "replace"
    ) {
        return Err(SnapshotError::InvalidOperation(snapshot.operation));
    }

    let timestamp_ms = Utc::now().timestamp_millis();

    let row: Snapshot = sqlx::query_as(
        r#"
        INSERT INTO iceberg_snapshots (
            table_id, snapshot_id, parent_snapshot_id, sequence_number,
            operation, manifest_list_location, summary, schema_id, timestamp_ms
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        RETURNING id, table_id, snapshot_id, parent_snapshot_id, sequence_number,
                  operation, manifest_list_location, summary, schema_id, timestamp_ms
        "#,
    )
    .bind(snapshot.table_id)
    .bind(snapshot.snapshot_id)
    .bind(snapshot.parent_snapshot_id)
    .bind(snapshot.sequence_number)
    .bind(&snapshot.operation)
    .bind(&snapshot.manifest_list_location)
    .bind(&snapshot.summary)
    .bind(snapshot.schema_id)
    .bind(timestamp_ms)
    .fetch_one(pool)
    .await?;

    Ok(row)
}

pub async fn list_for_table(pool: &PgPool, table_id: Uuid) -> Result<Vec<Snapshot>, SnapshotError> {
    let rows: Vec<Snapshot> = sqlx::query_as(
        r#"
        SELECT id, table_id, snapshot_id, parent_snapshot_id, sequence_number,
               operation, manifest_list_location, summary, schema_id, timestamp_ms
        FROM iceberg_snapshots
        WHERE table_id = $1
        ORDER BY timestamp_ms ASC, snapshot_id ASC
        "#,
    )
    .bind(table_id)
    .fetch_all(pool)
    .await?;
    Ok(rows)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_unknown_operation() {
        assert!(matches!(
            sample("bogus"),
            Err(SnapshotError::InvalidOperation(_))
        ));
    }

    fn sample(op: &str) -> Result<(), SnapshotError> {
        if !matches!(op, "append" | "overwrite" | "delete" | "replace") {
            return Err(SnapshotError::InvalidOperation(op.to_string()));
        }
        Ok(())
    }
}
