//! Iceberg tables — Postgres-backed catalog primitives.
//!
//! The on-disk source of truth for an Iceberg table is its
//! `metadata.json` file in object storage. The catalog augments that
//! with a queryable Postgres mirror so the Foundry UI / SQL warehouse
//! integration can paginate, filter and sort without round-tripping
//! every metadata file.

use chrono::{DateTime, Utc};
use serde_json::Value;
use sqlx::{FromRow, PgPool};
use uuid::Uuid;

use super::namespace::{Namespace, decode_path, encode_path};

#[derive(Debug, Clone, FromRow)]
pub struct IcebergTableRow {
    pub id: Uuid,
    pub rid: String,
    pub namespace_id: Uuid,
    pub name: String,
    pub table_uuid: String,
    pub format_version: i32,
    pub location: String,
    pub current_snapshot_id: Option<i64>,
    pub current_metadata_location: Option<String>,
    pub last_sequence_number: i64,
    pub partition_spec: Value,
    pub schema_json: Value,
    pub sort_order: Value,
    pub properties: Value,
    pub markings: Vec<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// In-memory representation enriched with the parent namespace path.
/// Hydrated by [`fetch`] / [`list_in_namespace`].
#[derive(Debug, Clone)]
pub struct IcebergTable {
    pub id: Uuid,
    pub rid: String,
    pub namespace_id: Uuid,
    pub namespace_path: Vec<String>,
    pub name: String,
    pub table_uuid: String,
    pub format_version: i32,
    pub location: String,
    pub current_snapshot_id: Option<i64>,
    pub current_metadata_location: Option<String>,
    pub last_sequence_number: i64,
    pub partition_spec: Value,
    pub schema_json: Value,
    pub sort_order: Value,
    pub properties: Value,
    pub markings: Vec<String>,
}

impl IcebergTable {
    fn from_row(row: IcebergTableRow, namespace: &Namespace) -> Self {
        Self {
            id: row.id,
            rid: row.rid,
            namespace_id: row.namespace_id,
            namespace_path: decode_path(&namespace.name),
            name: row.name,
            table_uuid: row.table_uuid,
            format_version: row.format_version,
            location: row.location,
            current_snapshot_id: row.current_snapshot_id,
            current_metadata_location: row.current_metadata_location,
            last_sequence_number: row.last_sequence_number,
            partition_spec: row.partition_spec,
            schema_json: row.schema_json,
            sort_order: row.sort_order,
            properties: row.properties,
            markings: row.markings,
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum TableError {
    #[error("table `{0}` already exists in namespace")]
    AlreadyExists(String),
    #[error("table `{0}` not found")]
    NotFound(String),
    #[error("invalid format-version {0}; catalog accepts 1, 2, 3")]
    InvalidFormatVersion(i32),
    #[error("schema is required")]
    SchemaMissing,
    #[error("commit requirements failed: {0}")]
    RequirementsFailed(String),
    #[error("namespace error: {0}")]
    Namespace(#[from] super::namespace::NamespaceError),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

#[derive(Debug, Clone)]
pub struct NewTable<'a> {
    pub namespace: &'a Namespace,
    pub name: &'a str,
    pub table_uuid: String,
    pub format_version: i32,
    pub location: String,
    pub schema_json: Value,
    pub partition_spec: Value,
    pub sort_order: Value,
    pub properties: Value,
    pub markings: Vec<String>,
}

pub async fn create(pool: &PgPool, new_table: NewTable<'_>) -> Result<IcebergTable, TableError> {
    if !(1..=3).contains(&new_table.format_version) {
        return Err(TableError::InvalidFormatVersion(new_table.format_version));
    }
    if new_table.schema_json.is_null() {
        return Err(TableError::SchemaMissing);
    }

    let exists: Option<i64> = sqlx::query_scalar(
        "SELECT 1 FROM iceberg_tables WHERE namespace_id = $1 AND name = $2",
    )
    .bind(new_table.namespace.id)
    .bind(new_table.name)
    .fetch_optional(pool)
    .await?;
    if exists.is_some() {
        return Err(TableError::AlreadyExists(new_table.name.to_string()));
    }

    let id = Uuid::now_v7();
    let row: IcebergTableRow = sqlx::query_as(
        r#"
        INSERT INTO iceberg_tables (
            id, namespace_id, name, table_uuid, format_version, location,
            partition_spec, schema_json, sort_order, properties, markings
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING id, rid, namespace_id, name, table_uuid, format_version, location,
                  current_snapshot_id, current_metadata_location, last_sequence_number,
                  partition_spec, schema_json, sort_order, properties, markings,
                  created_at, updated_at
        "#,
    )
    .bind(id)
    .bind(new_table.namespace.id)
    .bind(new_table.name)
    .bind(&new_table.table_uuid)
    .bind(new_table.format_version)
    .bind(&new_table.location)
    .bind(&new_table.partition_spec)
    .bind(&new_table.schema_json)
    .bind(&new_table.sort_order)
    .bind(&new_table.properties)
    .bind(&new_table.markings)
    .fetch_one(pool)
    .await?;

    Ok(IcebergTable::from_row(row, new_table.namespace))
}

pub async fn list_in_namespace(
    pool: &PgPool,
    namespace: &Namespace,
) -> Result<Vec<IcebergTable>, TableError> {
    let rows: Vec<IcebergTableRow> = sqlx::query_as(
        r#"
        SELECT id, rid, namespace_id, name, table_uuid, format_version, location,
               current_snapshot_id, current_metadata_location, last_sequence_number,
               partition_spec, schema_json, sort_order, properties, markings,
               created_at, updated_at
        FROM iceberg_tables
        WHERE namespace_id = $1
        ORDER BY name
        "#,
    )
    .bind(namespace.id)
    .fetch_all(pool)
    .await?;
    Ok(rows
        .into_iter()
        .map(|r| IcebergTable::from_row(r, namespace))
        .collect())
}

pub async fn fetch(
    pool: &PgPool,
    namespace: &Namespace,
    name: &str,
) -> Result<IcebergTable, TableError> {
    let row: Option<IcebergTableRow> = sqlx::query_as(
        r#"
        SELECT id, rid, namespace_id, name, table_uuid, format_version, location,
               current_snapshot_id, current_metadata_location, last_sequence_number,
               partition_spec, schema_json, sort_order, properties, markings,
               created_at, updated_at
        FROM iceberg_tables
        WHERE namespace_id = $1 AND name = $2
        "#,
    )
    .bind(namespace.id)
    .bind(name)
    .fetch_optional(pool)
    .await?;

    row.map(|r| IcebergTable::from_row(r, namespace))
        .ok_or_else(|| TableError::NotFound(format!("{}.{}", namespace.name, name)))
}

pub async fn fetch_by_rid(pool: &PgPool, rid: &str) -> Result<IcebergTable, TableError> {
    use sqlx::Row as _;
    let row = sqlx::query(
        r#"
        SELECT t.id, t.rid, t.namespace_id, t.name, t.table_uuid, t.format_version, t.location,
               t.current_snapshot_id, t.current_metadata_location, t.last_sequence_number,
               t.partition_spec, t.schema_json, t.sort_order, t.properties, t.markings,
               t.created_at, t.updated_at,
               n.name AS namespace_name
        FROM iceberg_tables t
        JOIN iceberg_namespaces n ON n.id = t.namespace_id
        WHERE t.rid = $1
        "#,
    )
    .bind(rid)
    .fetch_optional(pool)
    .await?;

    let row = row.ok_or_else(|| TableError::NotFound(rid.to_string()))?;
    let namespace_name: String = row.try_get("namespace_name")?;

    Ok(IcebergTable {
        id: row.try_get("id")?,
        rid: row.try_get("rid")?,
        namespace_id: row.try_get("namespace_id")?,
        namespace_path: decode_path(&namespace_name),
        name: row.try_get("name")?,
        table_uuid: row.try_get("table_uuid")?,
        format_version: row.try_get("format_version")?,
        location: row.try_get("location")?,
        current_snapshot_id: row.try_get("current_snapshot_id")?,
        current_metadata_location: row.try_get("current_metadata_location")?,
        last_sequence_number: row.try_get("last_sequence_number")?,
        partition_spec: row.try_get("partition_spec")?,
        schema_json: row.try_get("schema_json")?,
        sort_order: row.try_get("sort_order")?,
        properties: row.try_get("properties")?,
        markings: row.try_get("markings")?,
    })
}

pub async fn drop(
    pool: &PgPool,
    namespace: &Namespace,
    name: &str,
    _purge_requested: bool,
) -> Result<(), TableError> {
    let result = sqlx::query("DELETE FROM iceberg_tables WHERE namespace_id = $1 AND name = $2")
        .bind(namespace.id)
        .bind(name)
        .execute(pool)
        .await?;

    if result.rows_affected() == 0 {
        return Err(TableError::NotFound(format!("{}.{}", namespace.name, name)));
    }
    Ok(())
}

/// Apply a CommitTable update (the spec's `requirements` + `updates` body).
///
/// The catalog only enforces a small subset of requirements today
/// (`assert-uuid`, `assert-current-schema-id`, `assert-ref-snapshot-id`).
/// The remaining ones are accepted as no-ops so PyIceberg / Spark
/// commits succeed end-to-end against the Beta catalog.
pub async fn apply_commit(
    pool: &PgPool,
    table: &IcebergTable,
    requirements: &[Value],
    updates: &[Value],
) -> Result<IcebergTable, TableError> {
    for req in requirements {
        let kind = req.get("type").and_then(Value::as_str).unwrap_or_default();
        match kind {
            "assert-uuid" => {
                let expected = req.get("uuid").and_then(Value::as_str).unwrap_or_default();
                if expected != table.table_uuid {
                    return Err(TableError::RequirementsFailed(format!(
                        "assert-uuid: expected {expected}, found {}",
                        table.table_uuid
                    )));
                }
            }
            "assert-current-schema-id" => {
                let expected = req
                    .get("current-schema-id")
                    .and_then(Value::as_i64)
                    .unwrap_or_default();
                let current = table
                    .schema_json
                    .get("schema-id")
                    .and_then(Value::as_i64)
                    .unwrap_or(0);
                if expected != current {
                    return Err(TableError::RequirementsFailed(format!(
                        "assert-current-schema-id: expected {expected}, found {current}"
                    )));
                }
            }
            "assert-ref-snapshot-id" => {
                let ref_name = req.get("ref").and_then(Value::as_str).unwrap_or("main");
                let expected = req.get("snapshot-id").and_then(Value::as_i64);
                if ref_name == "main" && expected != table.current_snapshot_id {
                    return Err(TableError::RequirementsFailed(format!(
                        "assert-ref-snapshot-id: ref `main` expected {expected:?}, found {:?}",
                        table.current_snapshot_id
                    )));
                }
            }
            _ => {
                tracing::debug!(kind, "ignoring unsupported commit requirement");
            }
        }
    }

    let mut next_schema = table.schema_json.clone();
    let mut next_properties = table.properties.clone();
    let mut next_partition = table.partition_spec.clone();
    let mut next_sort = table.sort_order.clone();

    for update in updates {
        let action = update.get("action").and_then(Value::as_str).unwrap_or_default();
        match action {
            "add-schema" => {
                if let Some(schema) = update.get("schema").cloned() {
                    next_schema = schema;
                }
            }
            "set-properties" => {
                if let Some(updates_map) = update.get("updates").and_then(Value::as_object) {
                    let mut current = next_properties
                        .as_object()
                        .cloned()
                        .unwrap_or_else(serde_json::Map::new);
                    for (k, v) in updates_map.iter() {
                        current.insert(k.clone(), v.clone());
                    }
                    next_properties = Value::Object(current);
                }
            }
            "remove-properties" => {
                if let Some(removals) = update.get("removals").and_then(Value::as_array) {
                    if let Some(mut current) = next_properties
                        .as_object()
                        .cloned()
                        .map(serde_json::Map::from_iter)
                    {
                        for k in removals.iter().filter_map(|v| v.as_str()) {
                            current.remove(k);
                        }
                        next_properties = Value::Object(current);
                    }
                }
            }
            "add-partition-spec" => {
                if let Some(spec) = update.get("spec").cloned() {
                    next_partition = spec;
                }
            }
            "add-sort-order" => {
                if let Some(order) = update.get("sort-order").cloned() {
                    next_sort = order;
                }
            }
            _ => {
                tracing::debug!(action, "ignoring unsupported commit update");
            }
        }
    }

    let row: IcebergTableRow = sqlx::query_as(
        r#"
        UPDATE iceberg_tables
        SET schema_json = $2,
            properties = $3,
            partition_spec = $4,
            sort_order = $5,
            updated_at = NOW()
        WHERE id = $1
        RETURNING id, rid, namespace_id, name, table_uuid, format_version, location,
                  current_snapshot_id, current_metadata_location, last_sequence_number,
                  partition_spec, schema_json, sort_order, properties, markings,
                  created_at, updated_at
        "#,
    )
    .bind(table.id)
    .bind(&next_schema)
    .bind(&next_properties)
    .bind(&next_partition)
    .bind(&next_sort)
    .fetch_one(pool)
    .await?;

    Ok(IcebergTable {
        namespace_path: table.namespace_path.clone(),
        ..IcebergTable::from_row(
            row,
            // Synthesise a namespace stub good enough for `from_row`'s
            // path lookup; we already know the path so we don't need to
            // round-trip Postgres again.
            &Namespace {
                id: table.namespace_id,
                project_rid: String::new(),
                name: encode_path(&table.namespace_path),
                parent_namespace_id: None,
                properties: serde_json::Value::Null,
                created_at: chrono::Utc::now(),
                created_by: Uuid::nil(),
            },
        )
    })
}

/// Update `current_snapshot_id`, `current_metadata_location` and
/// `last_sequence_number` after a successful append/overwrite/replace.
pub async fn advance_snapshot(
    pool: &PgPool,
    table_id: Uuid,
    snapshot_id: i64,
    metadata_location: &str,
    sequence_number: i64,
) -> Result<(), TableError> {
    sqlx::query(
        r#"
        UPDATE iceberg_tables
        SET current_snapshot_id = $2,
            current_metadata_location = $3,
            last_sequence_number = GREATEST(last_sequence_number, $4),
            updated_at = NOW()
        WHERE id = $1
        "#,
    )
    .bind(table_id)
    .bind(snapshot_id)
    .bind(metadata_location)
    .bind(sequence_number)
    .execute(pool)
    .await?;
    Ok(())
}
