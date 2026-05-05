//! Iceberg branches and tags — refs that pin a snapshot for a name.
//!
//! Foundry treats `master` and `main` as equivalent at the catalog
//! layer (per `Iceberg tables.md` § "Default branches"); the dataset
//! adapter handles the rewrite when SQL writers point at `master`.

use chrono::{DateTime, Utc};
use serde_json::Value;
use sqlx::{FromRow, PgPool};
use uuid::Uuid;

#[derive(Debug, Clone, FromRow)]
pub struct TableBranch {
    pub id: Uuid,
    pub table_id: Uuid,
    pub name: String,
    pub kind: String,
    pub snapshot_id: i64,
    pub max_ref_age_ms: Option<i64>,
    pub max_snapshot_age_ms: Option<i64>,
    pub min_snapshots_to_keep: Option<i32>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, thiserror::Error)]
pub enum BranchError {
    #[error("ref `{0}` not found")]
    NotFound(String),
    #[error("invalid ref kind `{0}` (must be branch or tag)")]
    InvalidKind(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

pub async fn upsert(
    pool: &PgPool,
    table_id: Uuid,
    name: &str,
    snapshot_id: i64,
    kind: &str,
) -> Result<TableBranch, BranchError> {
    if !matches!(kind, "branch" | "tag") {
        return Err(BranchError::InvalidKind(kind.to_string()));
    }
    // Foundry alias: master == main at the catalog level.
    let canonical = if name == "master" { "main" } else { name };
    let row: TableBranch = sqlx::query_as(
        r#"
        INSERT INTO iceberg_table_branches (id, table_id, name, kind, snapshot_id)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (table_id, name) DO UPDATE
            SET snapshot_id = EXCLUDED.snapshot_id,
                kind = EXCLUDED.kind
        RETURNING id, table_id, name, kind, snapshot_id, max_ref_age_ms,
                  max_snapshot_age_ms, min_snapshots_to_keep, created_at
        "#,
    )
    .bind(Uuid::now_v7())
    .bind(table_id)
    .bind(canonical)
    .bind(kind)
    .bind(snapshot_id)
    .fetch_one(pool)
    .await?;
    Ok(row)
}

pub async fn list(pool: &PgPool, table_id: Uuid) -> Result<Vec<TableBranch>, BranchError> {
    let rows: Vec<TableBranch> = sqlx::query_as(
        r#"
        SELECT id, table_id, name, kind, snapshot_id, max_ref_age_ms,
               max_snapshot_age_ms, min_snapshots_to_keep, created_at
        FROM iceberg_table_branches
        WHERE table_id = $1
        ORDER BY name
        "#,
    )
    .bind(table_id)
    .fetch_all(pool)
    .await?;
    Ok(rows)
}

/// Render a `refs` map as embedded inside `metadata.json`.
pub fn refs_map(branches: &[TableBranch]) -> Value {
    let mut map = serde_json::Map::new();
    for b in branches {
        let mut entry = serde_json::Map::from_iter([
            (
                "snapshot-id".to_string(),
                Value::Number(b.snapshot_id.into()),
            ),
            ("type".to_string(), Value::String(b.kind.clone())),
        ]);
        if let Some(ms) = b.max_ref_age_ms {
            entry.insert("max-ref-age-ms".to_string(), Value::Number(ms.into()));
        }
        if let Some(ms) = b.max_snapshot_age_ms {
            entry.insert("max-snapshot-age-ms".to_string(), Value::Number(ms.into()));
        }
        if let Some(n) = b.min_snapshots_to_keep {
            entry.insert("min-snapshots-to-keep".to_string(), Value::Number(n.into()));
        }
        map.insert(b.name.clone(), Value::Object(entry));
    }
    Value::Object(map)
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;

    #[test]
    fn refs_map_includes_kind_and_snapshot_id() {
        let branches = vec![TableBranch {
            id: Uuid::nil(),
            table_id: Uuid::nil(),
            name: "main".to_string(),
            kind: "branch".to_string(),
            snapshot_id: 42,
            max_ref_age_ms: Some(1_000),
            max_snapshot_age_ms: None,
            min_snapshots_to_keep: None,
            created_at: Utc::now(),
        }];

        let map = refs_map(&branches);
        assert_eq!(map["main"]["snapshot-id"], serde_json::json!(42));
        assert_eq!(map["main"]["type"], serde_json::json!("branch"));
        assert_eq!(map["main"]["max-ref-age-ms"], serde_json::json!(1_000));
    }
}
