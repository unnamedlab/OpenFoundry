//! Postgres persistence for declarative [`crate::proto::IngestJob`] intent.
//!
//! The schema lives in `migrations/20260429120000_ingest_jobs.sql`.

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};
use uuid::Uuid;

use crate::proto::{IngestJob, IngestJobSpec};

/// Low-frequency control-plane status values persisted as desired-state
/// metadata. These are no longer the authoritative runtime state.
pub mod status {
    pub const DESIRED: &str = "desired";
    pub const MATERIALIZED: &str = "materialized";
    pub const FAILED: &str = "failed";
}

#[derive(Debug, Clone)]
pub struct IngestJobRecord {
    pub id: Uuid,
    pub name: String,
    pub namespace: String,
    pub spec: IngestJobSpec,
    pub status: String,
    pub kafka_connector_name: Option<String>,
    pub flink_deployment_name: Option<String>,
    pub error: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<IngestJobRecord> for IngestJob {
    fn from(record: IngestJobRecord) -> Self {
        IngestJob {
            id: record.id.to_string(),
            spec: Some(record.spec),
            status: record.status,
            kafka_connector_name: record.kafka_connector_name.unwrap_or_default(),
            flink_deployment_name: record.flink_deployment_name.unwrap_or_default(),
            created_at: record.created_at.to_rfc3339(),
            updated_at: record.updated_at.to_rfc3339(),
            error: record.error.unwrap_or_default(),
        }
    }
}

/// Insert (or upsert by `(namespace, name)`) a fresh desired-state job and return
/// the persisted record.
pub async fn insert_job(
    pool: &PgPool,
    namespace: &str,
    name: &str,
    spec: &IngestJobSpec,
) -> Result<IngestJobRecord> {
    let id = Uuid::now_v7();
    let spec_json = serde_json::to_value(spec).context("serialize IngestJobSpec")?;
    let row = sqlx::query(
        r#"
        INSERT INTO ingest_jobs (id, name, namespace, spec, status)
        VALUES ($1, $2, $3, $4::jsonb, $5)
        ON CONFLICT (namespace, name) DO UPDATE
          SET spec = EXCLUDED.spec,
              status = $5,
              error = NULL,
              updated_at = NOW()
        RETURNING id, name, namespace, spec, status,
                  kafka_connector_name, flink_deployment_name, error,
                  created_at, updated_at
        "#,
    )
    .bind(id)
    .bind(name)
    .bind(namespace)
    .bind(&spec_json)
    .bind(status::DESIRED)
    .fetch_one(pool)
    .await
    .context("insert ingest_job")?;
    row_to_record(&row)
}

/// Record the names of the cluster resources that represent this desired job.
pub async fn mark_materialized(
    pool: &PgPool,
    id: Uuid,
    kafka_connector_name: &str,
    flink_deployment_name: Option<&str>,
) -> Result<()> {
    sqlx::query(
        r#"
        UPDATE ingest_jobs
        SET status = $2,
            kafka_connector_name = $3,
            flink_deployment_name = $4,
            error = NULL,
            updated_at = NOW()
        WHERE id = $1
        "#,
    )
    .bind(id)
    .bind(status::DESIRED)
    .bind(kafka_connector_name)
    .bind(flink_deployment_name)
    .execute(pool)
    .await
    .context("mark_materialized")?;
    Ok(())
}

/// Legacy helper retained for compatibility with pre-runtime-store rows.
pub async fn mark_failed(pool: &PgPool, id: Uuid, error: &str) -> Result<()> {
    sqlx::query(
        r#"
        UPDATE ingest_jobs
        SET status = $2, error = $3, updated_at = NOW()
        WHERE id = $1
        "#,
    )
    .bind(id)
    .bind(status::FAILED)
    .bind(error)
    .execute(pool)
    .await
    .context("mark_failed")?;
    Ok(())
}

/// Fetch a single job by id. Returns `Ok(None)` when no row matches.
pub async fn get_job(pool: &PgPool, id: Uuid) -> Result<Option<IngestJobRecord>> {
    let row = sqlx::query(
        r#"
        SELECT id, name, namespace, spec, status,
               kafka_connector_name, flink_deployment_name, error,
               created_at, updated_at
        FROM ingest_jobs WHERE id = $1
        "#,
    )
    .bind(id)
    .fetch_optional(pool)
    .await
    .context("get_job")?;
    row.as_ref().map(row_to_record).transpose()
}

/// List every job, newest first.
pub async fn list_jobs(pool: &PgPool) -> Result<Vec<IngestJobRecord>> {
    let rows = sqlx::query(
        r#"
        SELECT id, name, namespace, spec, status,
               kafka_connector_name, flink_deployment_name, error,
               created_at, updated_at
        FROM ingest_jobs ORDER BY created_at DESC
        "#,
    )
    .fetch_all(pool)
    .await
    .context("list_jobs")?;
    rows.iter().map(row_to_record).collect()
}

/// Delete a job by id and return the previously persisted record so callers
/// know which Kubernetes resources to clean up.
pub async fn delete_job(pool: &PgPool, id: Uuid) -> Result<Option<IngestJobRecord>> {
    let row = sqlx::query(
        r#"
        DELETE FROM ingest_jobs WHERE id = $1
        RETURNING id, name, namespace, spec, status,
                  kafka_connector_name, flink_deployment_name, error,
                  created_at, updated_at
        "#,
    )
    .bind(id)
    .fetch_optional(pool)
    .await
    .context("delete_job")?;
    row.as_ref().map(row_to_record).transpose()
}

/// List jobs that the reconcile loop should re-process.
///
/// Reconciliation is now driven from the declarative inventory itself rather
/// than a Postgres runtime status column, so every row remains eligible.
pub async fn list_reconcilable(pool: &PgPool) -> Result<Vec<IngestJobRecord>> {
    let rows = sqlx::query(
        r#"
        SELECT id, name, namespace, spec, status,
               kafka_connector_name, flink_deployment_name, error,
               created_at, updated_at
        FROM ingest_jobs
        ORDER BY updated_at ASC
        "#,
    )
    .fetch_all(pool)
    .await
    .context("list_reconcilable")?;
    rows.iter().map(row_to_record).collect()
}

fn row_to_record(row: &sqlx::postgres::PgRow) -> Result<IngestJobRecord> {
    let spec_value: serde_json::Value = row.try_get("spec")?;
    let spec: IngestJobSpec =
        serde_json::from_value(spec_value).context("deserialize stored spec")?;
    Ok(IngestJobRecord {
        id: row.try_get("id")?,
        name: row.try_get("name")?,
        namespace: row.try_get("namespace")?,
        spec,
        status: row.try_get("status")?,
        kafka_connector_name: row.try_get("kafka_connector_name")?,
        flink_deployment_name: row.try_get("flink_deployment_name")?,
        error: row.try_get("error")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
    })
}
