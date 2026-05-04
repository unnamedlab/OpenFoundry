//! Postgres-backed [`IdempotencyStore`].
//!
//! Uses `INSERT … ON CONFLICT DO NOTHING RETURNING event_id` — a
//! single round-trip that gives strict atomicity (the row either
//! materialises or is silently rejected) and lets us tell first vs.
//! repeat by inspecting whether the `RETURNING` clause produced a
//! row.

use sqlx::PgPool;
use uuid::Uuid;

use crate::{IdempotencyError, IdempotencyStore, Outcome};

/// Postgres-backed [`IdempotencyStore`].
///
/// `table` is the fully-qualified `schema.table` of the dedup table.
/// It MUST already exist with the shape declared in
/// `migrations/0001_processed_events.sql` (a `uuid PRIMARY KEY`
/// column called `event_id` plus a `processed_at timestamptz NOT
/// NULL DEFAULT now()` column). The schema/table is `&'static str`
/// so it can never be user-controlled at runtime — this prevents
/// SQL injection through the table-name slot, which cannot be
/// parameterised in Postgres.
#[derive(Debug, Clone)]
pub struct PgIdempotencyStore {
    pool: PgPool,
    table: &'static str,
}

impl PgIdempotencyStore {
    /// Build a store that writes to `table` (e.g. `idem.processed_events`).
    pub fn new(pool: PgPool, table: &'static str) -> Self {
        Self { pool, table }
    }

    /// The fully-qualified table name this store writes to.
    pub fn table(&self) -> &'static str {
        self.table
    }

    /// Underlying connection pool, for tests and shutdown.
    pub fn pool(&self) -> &PgPool {
        &self.pool
    }
}

#[async_trait::async_trait]
impl IdempotencyStore for PgIdempotencyStore {
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError> {
        // The `RETURNING event_id` clause emits exactly one row when
        // the INSERT applied and zero rows when it was a duplicate —
        // a single round-trip with no race window between "is the
        // row there?" and "record the row".
        //
        // The `{table}` interpolation is safe because `self.table`
        // is `&'static str`, picked by the operator at deploy time
        // and never user-controlled. We cannot bind table names as
        // parameters in Postgres.
        let sql = format!(
            "INSERT INTO {table} (event_id) VALUES ($1) \
             ON CONFLICT (event_id) DO NOTHING \
             RETURNING event_id",
            table = self.table
        );

        let inserted: Option<(Uuid,)> = sqlx::query_as(&sql)
            .bind(event_id)
            .fetch_optional(&self.pool)
            .await
            .map_err(IdempotencyError::backend)?;

        Ok(if inserted.is_some() {
            Outcome::FirstSeen
        } else {
            Outcome::AlreadyProcessed
        })
    }
}
