//! Iceberg namespaces (REST Catalog § Namespaces).
//!
//! A namespace is a hierarchical container for tables. The Iceberg spec
//! represents the path as a JSON array of segments (`["analytics", "events"]`)
//! while clients typically dot-encode it on the wire (`analytics.events`).
//! This module owns both representations plus the Postgres CRUD.

use chrono::{DateTime, Utc};
use serde_json::Value;
use sqlx::{FromRow, PgPool, Row};
use uuid::Uuid;

#[derive(Debug, Clone, FromRow)]
pub struct Namespace {
    pub id: Uuid,
    pub project_rid: String,
    pub name: String,
    pub parent_namespace_id: Option<Uuid>,
    pub properties: Value,
    pub created_at: DateTime<Utc>,
    pub created_by: Uuid,
}

#[derive(Debug, thiserror::Error)]
pub enum NamespaceError {
    #[error("namespace `{0}` already exists")]
    AlreadyExists(String),
    #[error("namespace `{0}` does not exist")]
    NotExists(String),
    #[error("namespace `{0}` is not empty (drop tables first)")]
    NotEmpty(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

/// Encode a namespace path (`["a", "b"]`) into the dotted form Foundry
/// stores in `name`. Iceberg recommends `0x1F` (unit separator) on the
/// wire but in practice every client sends dots.
pub fn encode_path(parts: &[String]) -> String {
    parts.join(".")
}

pub fn decode_path(name: &str) -> Vec<String> {
    if name.is_empty() {
        return Vec::new();
    }
    name.split('.').map(str::to_string).collect()
}

/// Insert a namespace; returns `AlreadyExists` if `(project_rid, name)`
/// is taken. Properties default to an empty JSON object.
pub async fn create(
    pool: &PgPool,
    project_rid: &str,
    path: &[String],
    properties: Value,
    created_by: Uuid,
    parent: Option<Uuid>,
) -> Result<Namespace, NamespaceError> {
    let id = Uuid::now_v7();
    let name = encode_path(path);

    // Pre-check for existence — Postgres `ON CONFLICT DO NOTHING` would
    // suppress the row, but we want to return AlreadyExists explicitly.
    let exists: Option<i64> = sqlx::query_scalar(
        "SELECT 1 FROM iceberg_namespaces WHERE project_rid = $1 AND name = $2",
    )
    .bind(project_rid)
    .bind(&name)
    .fetch_optional(pool)
    .await?;
    if exists.is_some() {
        return Err(NamespaceError::AlreadyExists(name));
    }

    let row: Namespace = sqlx::query_as(
        r#"
        INSERT INTO iceberg_namespaces (id, project_rid, name, parent_namespace_id, properties, created_by)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id, project_rid, name, parent_namespace_id, properties, created_at, created_by
        "#,
    )
    .bind(id)
    .bind(project_rid)
    .bind(&name)
    .bind(parent)
    .bind(&properties)
    .bind(created_by)
    .fetch_one(pool)
    .await?;
    Ok(row)
}

/// List namespaces under `project_rid`. When `parent_path` is `Some`,
/// only direct children are returned (the spec's `parent` query param).
pub async fn list(
    pool: &PgPool,
    project_rid: &str,
    parent_path: Option<&[String]>,
) -> Result<Vec<Namespace>, NamespaceError> {
    let rows = match parent_path {
        Some(parts) => {
            let parent_name = encode_path(parts);
            sqlx::query_as::<_, Namespace>(
                r#"
                SELECT id, project_rid, name, parent_namespace_id, properties, created_at, created_by
                FROM iceberg_namespaces
                WHERE project_rid = $1
                  AND parent_namespace_id = (
                      SELECT id FROM iceberg_namespaces
                      WHERE project_rid = $1 AND name = $2
                  )
                ORDER BY name
                "#,
            )
            .bind(project_rid)
            .bind(&parent_name)
            .fetch_all(pool)
            .await?
        }
        None => {
            sqlx::query_as::<_, Namespace>(
                r#"
                SELECT id, project_rid, name, parent_namespace_id, properties, created_at, created_by
                FROM iceberg_namespaces
                WHERE project_rid = $1
                  AND parent_namespace_id IS NULL
                ORDER BY name
                "#,
            )
            .bind(project_rid)
            .fetch_all(pool)
            .await?
        }
    };

    Ok(rows)
}

pub async fn fetch(
    pool: &PgPool,
    project_rid: &str,
    path: &[String],
) -> Result<Namespace, NamespaceError> {
    let name = encode_path(path);
    let row: Option<Namespace> = sqlx::query_as(
        r#"
        SELECT id, project_rid, name, parent_namespace_id, properties, created_at, created_by
        FROM iceberg_namespaces
        WHERE project_rid = $1 AND name = $2
        "#,
    )
    .bind(project_rid)
    .bind(&name)
    .fetch_optional(pool)
    .await?;

    row.ok_or(NamespaceError::NotExists(name))
}

pub async fn drop(
    pool: &PgPool,
    project_rid: &str,
    path: &[String],
) -> Result<(), NamespaceError> {
    let ns = fetch(pool, project_rid, path).await?;

    // Spec: 409 Conflict when the namespace still contains tables.
    let row =
        sqlx::query("SELECT COUNT(*)::BIGINT AS count FROM iceberg_tables WHERE namespace_id = $1")
            .bind(ns.id)
            .fetch_one(pool)
            .await?;
    let table_count: i64 = row.try_get("count")?;
    if table_count > 0 {
        return Err(NamespaceError::NotEmpty(ns.name));
    }

    sqlx::query("DELETE FROM iceberg_namespaces WHERE id = $1")
        .bind(ns.id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn replace_properties(
    pool: &PgPool,
    project_rid: &str,
    path: &[String],
    properties: Value,
) -> Result<Namespace, NamespaceError> {
    let ns = fetch(pool, project_rid, path).await?;
    let row: Namespace = sqlx::query_as(
        r#"
        UPDATE iceberg_namespaces
        SET properties = $2
        WHERE id = $1
        RETURNING id, project_rid, name, parent_namespace_id, properties, created_at, created_by
        "#,
    )
    .bind(ns.id)
    .bind(&properties)
    .fetch_one(pool)
    .await?;
    Ok(row)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_dot_encoded_paths() {
        let parts = vec!["analytics".to_string(), "events".to_string()];
        let encoded = encode_path(&parts);
        assert_eq!(encoded, "analytics.events");
        assert_eq!(decode_path(&encoded), parts);
    }

    #[test]
    fn empty_path_decodes_to_empty_vec() {
        assert!(decode_path("").is_empty());
    }
}
