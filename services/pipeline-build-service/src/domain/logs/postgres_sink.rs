//! Postgres-backed [`super::LogSink`] persisting to `job_logs`.
//!
//! Looks up the job by RID once per emit and inserts into the
//! append-only table. The DB-assigned `BIGSERIAL sequence` is read
//! back so callers (and the broadcast sink) see the monotonic id.

use async_trait::async_trait;
use sqlx::PgPool;

use super::{LogEntry, LogSink, LogSinkError};
use crate::domain::metrics;

pub struct PostgresLogSink {
    pool: PgPool,
}

impl PostgresLogSink {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl LogSink for PostgresLogSink {
    async fn emit(&self, entry: LogEntry) -> Result<i64, LogSinkError> {
        let job_id: Option<(uuid::Uuid,)> =
            sqlx::query_as("SELECT id FROM jobs WHERE rid = $1")
                .bind(&entry.job_rid)
                .fetch_optional(&self.pool)
                .await?;
        let Some((job_id,)) = job_id else {
            return Err(LogSinkError::JobNotFound(entry.job_rid));
        };

        let id = uuid::Uuid::now_v7();
        let row: (i64,) = sqlx::query_as(
            r#"INSERT INTO job_logs (id, job_id, ts, level, message, params)
               VALUES ($1, $2, $3, $4, $5, $6)
               RETURNING sequence"#,
        )
        .bind(id)
        .bind(job_id)
        .bind(entry.ts)
        .bind(entry.level.as_str())
        .bind(&entry.message)
        .bind(entry.params.as_ref())
        .fetch_one(&self.pool)
        .await?;

        metrics::record_log_emitted(entry.level);
        Ok(row.0)
    }
}
