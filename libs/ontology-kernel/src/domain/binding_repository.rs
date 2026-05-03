//! PostgreSQL-backed repository for object type bindings.
//!
//! Bindings are declarative control-plane rows. Materialised objects already
//! flow through `ObjectStore`; this repository keeps the residual PG access out
//! of HTTP handlers while S1 finishes the Cassandra runtime migration.

use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

use crate::models::object_type_binding::{
    ObjectTypeBinding, ObjectTypeBindingRow, ObjectTypeBindingSyncMode,
};

#[derive(Debug, thiserror::Error)]
pub enum BindingRepoError {
    #[error(transparent)]
    Sql(#[from] sqlx::Error),
    #[error("{0}")]
    Decode(String),
}

impl BindingRepoError {
    pub fn constraint(&self) -> Option<&str> {
        match self {
            Self::Sql(sqlx::Error::Database(error)) => error.constraint(),
            _ => None,
        }
    }
}

pub struct CreateBindingInput<'a> {
    pub id: Uuid,
    pub object_type_id: Uuid,
    pub dataset_id: Uuid,
    pub dataset_branch: Option<&'a str>,
    pub dataset_version: Option<i32>,
    pub primary_key_column: &'a str,
    pub property_mapping: &'a Value,
    pub sync_mode: ObjectTypeBindingSyncMode,
    pub default_marking: &'a str,
    pub preview_limit: i32,
    pub owner_id: Uuid,
}

pub struct UpdateBindingInput<'a> {
    pub binding_id: Uuid,
    pub dataset_branch: Option<&'a str>,
    pub dataset_version: Option<i32>,
    pub primary_key_column: &'a str,
    pub property_mapping: &'a Value,
    pub sync_mode: ObjectTypeBindingSyncMode,
    pub default_marking: &'a str,
    pub preview_limit: i32,
}

fn decode(row: ObjectTypeBindingRow) -> Result<ObjectTypeBinding, BindingRepoError> {
    ObjectTypeBinding::try_from(row).map_err(BindingRepoError::Decode)
}

pub async fn load_binding(
    db: &PgPool,
    object_type_id: Uuid,
    binding_id: Uuid,
) -> Result<Option<ObjectTypeBinding>, BindingRepoError> {
    let row = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"SELECT id, object_type_id, dataset_id, dataset_branch, dataset_version,
                  primary_key_column, property_mapping, sync_mode, default_marking,
                  preview_limit, owner_id, last_materialized_at, last_run_status,
                  last_run_summary, created_at, updated_at
           FROM object_type_bindings
           WHERE id = $1 AND object_type_id = $2"#,
    )
    .bind(binding_id)
    .bind(object_type_id)
    .fetch_optional(db)
    .await?;

    row.map(decode).transpose()
}

pub async fn create_binding(
    db: &PgPool,
    input: CreateBindingInput<'_>,
) -> Result<ObjectTypeBinding, BindingRepoError> {
    let row = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"INSERT INTO object_type_bindings (
               id, object_type_id, dataset_id, dataset_branch, dataset_version,
               primary_key_column, property_mapping, sync_mode, default_marking,
               preview_limit, owner_id
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
           RETURNING id, object_type_id, dataset_id, dataset_branch, dataset_version,
                     primary_key_column, property_mapping, sync_mode, default_marking,
                     preview_limit, owner_id, last_materialized_at, last_run_status,
                     last_run_summary, created_at, updated_at"#,
    )
    .bind(input.id)
    .bind(input.object_type_id)
    .bind(input.dataset_id)
    .bind(input.dataset_branch)
    .bind(input.dataset_version)
    .bind(input.primary_key_column)
    .bind(input.property_mapping)
    .bind(input.sync_mode.as_str())
    .bind(input.default_marking)
    .bind(input.preview_limit)
    .bind(input.owner_id)
    .fetch_one(db)
    .await?;

    decode(row)
}

pub async fn list_bindings(
    db: &PgPool,
    object_type_id: Uuid,
) -> Result<Vec<ObjectTypeBinding>, BindingRepoError> {
    let rows = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"SELECT id, object_type_id, dataset_id, dataset_branch, dataset_version,
                  primary_key_column, property_mapping, sync_mode, default_marking,
                  preview_limit, owner_id, last_materialized_at, last_run_status,
                  last_run_summary, created_at, updated_at
           FROM object_type_bindings
           WHERE object_type_id = $1
           ORDER BY created_at DESC"#,
    )
    .bind(object_type_id)
    .fetch_all(db)
    .await?;

    rows.into_iter().map(decode).collect()
}

pub async fn update_binding(
    db: &PgPool,
    input: UpdateBindingInput<'_>,
) -> Result<ObjectTypeBinding, BindingRepoError> {
    let row = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"UPDATE object_type_bindings
           SET dataset_branch = $2,
               dataset_version = $3,
               primary_key_column = $4,
               property_mapping = $5,
               sync_mode = $6,
               default_marking = $7,
               preview_limit = $8,
               updated_at = NOW()
           WHERE id = $1
           RETURNING id, object_type_id, dataset_id, dataset_branch, dataset_version,
                     primary_key_column, property_mapping, sync_mode, default_marking,
                     preview_limit, owner_id, last_materialized_at, last_run_status,
                     last_run_summary, created_at, updated_at"#,
    )
    .bind(input.binding_id)
    .bind(input.dataset_branch)
    .bind(input.dataset_version)
    .bind(input.primary_key_column)
    .bind(input.property_mapping)
    .bind(input.sync_mode.as_str())
    .bind(input.default_marking)
    .bind(input.preview_limit)
    .fetch_one(db)
    .await?;

    decode(row)
}

pub async fn delete_binding(
    db: &PgPool,
    object_type_id: Uuid,
    binding_id: Uuid,
) -> Result<bool, BindingRepoError> {
    let result =
        sqlx::query("DELETE FROM object_type_bindings WHERE id = $1 AND object_type_id = $2")
            .bind(binding_id)
            .bind(object_type_id)
            .execute(db)
            .await?;
    Ok(result.rows_affected() > 0)
}

pub async fn record_materialization_result(
    db: &PgPool,
    binding_id: Uuid,
    status: &str,
    summary: &Value,
) -> Result<(), BindingRepoError> {
    sqlx::query(
        r#"UPDATE object_type_bindings
           SET last_materialized_at = NOW(),
               last_run_status = $2,
               last_run_summary = $3,
               updated_at = NOW()
           WHERE id = $1"#,
    )
    .bind(binding_id)
    .bind(status)
    .bind(summary)
    .execute(db)
    .await?;
    Ok(())
}
