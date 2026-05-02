//! Postgres-backed loader for `pg-policy.cedar_policies`.
//!
//! The expected schema (managed by the bootstrap migration in
//! `services/authorization-policy-service`):
//!
//! ```sql
//! CREATE TABLE IF NOT EXISTS cedar_policies (
//!     id          TEXT        PRIMARY KEY,
//!     version     INT         NOT NULL,
//!     source      TEXT        NOT NULL,
//!     description TEXT,
//!     active      BOOLEAN     NOT NULL DEFAULT TRUE,
//!     updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
//! );
//! ```
//!
//! Only the highest `version` per `id` is loaded, and only rows where
//! `active = TRUE`. This matches the contract documented in ADR-0027.

use anyhow::Context as _;
use sqlx::{PgPool, Row};

use crate::{PolicyRecord, PolicyStore, PolicyStoreError};

/// Postgres-backed loader for [`PolicyStore`].
#[derive(Clone)]
pub struct PgPolicyStore {
    pool: PgPool,
    table: String,
    store: PolicyStore,
}

impl PgPolicyStore {
    /// Build a `PgPolicyStore` against the default `cedar_policies`
    /// table. Use [`Self::with_table`] to override.
    pub fn new(pool: PgPool, store: PolicyStore) -> Self {
        Self {
            pool,
            table: "cedar_policies".to_owned(),
            store,
        }
    }

    /// Override the source table (defaults to `cedar_policies`).
    pub fn with_table(mut self, table: impl Into<String>) -> Self {
        self.table = table.into();
        self
    }

    /// Underlying [`PolicyStore`] handle. Cloning is cheap; keep the
    /// `PgPolicyStore` alive only on the loader thread.
    pub fn store(&self) -> PolicyStore {
        self.store.clone()
    }

    /// Read every active policy from Postgres and atomically swap them
    /// into the inner [`PolicyStore`].
    ///
    /// Idempotent — call from startup *and* from the NATS
    /// `authz.policy.changed` handler.
    pub async fn reload(&self) -> Result<usize, PolicyStoreError> {
        let records = self.fetch_active().await?;
        let count = records.len();
        self.store.replace_policies(&records).await?;
        tracing::info!(
            policies = count,
            table = %self.table,
            "cedar policies reloaded"
        );
        Ok(count)
    }

    async fn fetch_active(&self) -> Result<Vec<PolicyRecord>, PolicyStoreError> {
        // We deliberately build the SQL via format! so callers can
        // override the table name. The interpolation is restricted to
        // the table identifier — never user input — and is escaped
        // defensively below.
        let table = self.sanitised_table()?;
        let sql = format!(
            "SELECT DISTINCT ON (id) id, version, source, description \
               FROM {table} \
              WHERE active = TRUE \
              ORDER BY id, version DESC"
        );
        let rows = sqlx::query(&sql)
            .fetch_all(&self.pool)
            .await
            .map_err(|e| PolicyStoreError::Backend(anyhow::Error::from(e)))?;
        rows.into_iter()
            .map(|row| {
                Ok(PolicyRecord {
                    id: row
                        .try_get::<String, _>("id")
                        .context("column id")
                        .map_err(PolicyStoreError::Backend)?,
                    version: row
                        .try_get::<i32, _>("version")
                        .context("column version")
                        .map_err(PolicyStoreError::Backend)?,
                    source: row
                        .try_get::<String, _>("source")
                        .context("column source")
                        .map_err(PolicyStoreError::Backend)?,
                    description: row
                        .try_get::<Option<String>, _>("description")
                        .context("column description")
                        .map_err(PolicyStoreError::Backend)?,
                })
            })
            .collect()
    }

    /// Reject anything that is not a bare lowercase SQL identifier so
    /// the `format!` above can never emit a SQL injection vector.
    fn sanitised_table(&self) -> Result<&str, PolicyStoreError> {
        let valid = !self.table.is_empty()
            && self
                .table
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || c == '_');
        if valid {
            Ok(&self.table)
        } else {
            Err(PolicyStoreError::Backend(anyhow::anyhow!(
                "invalid table name `{}`",
                self.table
            )))
        }
    }
}
