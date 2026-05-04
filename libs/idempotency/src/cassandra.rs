//! Cassandra-backed [`IdempotencyStore`].
//!
//! Uses an LWT `INSERT … IF NOT EXISTS` at `LOCAL_SERIAL` — the only
//! Cassandra primitive that gives a true atomic check-and-record.
//! LWTs cost roughly 4× a regular write (Paxos round-trip), so reach
//! for the Postgres backend if Postgres is already on the consumer's
//! data path; pick this one when the consumer is Cassandra-native
//! (`object-database-service`, `audit-trail`, `action-log`, …).
//!
//! Rows expire via the table's `default_time_to_live = 2592000`
//! (30 days). Operators MUST keep that TTL ≥ the source Kafka
//! topic's retention or events delivered after the row TTLs out
//! will be reprocessed.

use std::sync::Arc;

use scylla::Session;
use scylla::frame::value::CqlTimestamp;
use scylla::statement::SerialConsistency;
use uuid::Uuid;

use crate::{IdempotencyError, IdempotencyStore, Outcome};

/// Cassandra-backed [`IdempotencyStore`].
///
/// `ks_table` is the fully-qualified `keyspace.table` of the dedup
/// table. It MUST already exist with the shape declared in
/// `migrations/0001_processed_events.cql` (`event_id uuid PRIMARY
/// KEY`, `processed_at timestamp`, `WITH default_time_to_live =
/// 2592000`). The slot is `&'static str` so it cannot be user-
/// controlled at runtime — Cassandra (like Postgres) will not bind
/// table or keyspace names as parameters.
#[derive(Clone)]
pub struct CassandraIdempotencyStore {
    session: Arc<Session>,
    ks_table: &'static str,
}

impl CassandraIdempotencyStore {
    /// Build a store that writes to `ks_table` (e.g.
    /// `idem.processed_events`).
    pub fn new(session: Arc<Session>, ks_table: &'static str) -> Self {
        Self { session, ks_table }
    }

    /// The fully-qualified `keyspace.table` this store writes to.
    pub fn ks_table(&self) -> &'static str {
        self.ks_table
    }

    /// Underlying session, for tests and advanced use.
    pub fn session(&self) -> &Arc<Session> {
        &self.session
    }
}

#[async_trait::async_trait]
impl IdempotencyStore for CassandraIdempotencyStore {
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError> {
        // `IF NOT EXISTS` makes this an LWT; the response carries an
        // `[applied]` boolean as its first column that's `true` when
        // the row was newly inserted, `false` when an existing row
        // already held this `event_id`.
        //
        // We pass `processed_at` explicitly (rather than relying on
        // a server-side `toTimestamp(now())`) so the value tracks the
        // wall-clock of the consumer that won the race — this lines
        // up with the Postgres backend, which uses the consumer's
        // session clock via `DEFAULT now()`.
        let cql = format!(
            "INSERT INTO {ks_table} (event_id, processed_at) VALUES (?, ?) IF NOT EXISTS",
            ks_table = self.ks_table
        );
        let now_ms = chrono::Utc::now().timestamp_millis();

        let prepared = self
            .session
            .prepare(cql)
            .await
            .map_err(IdempotencyError::backend)?;
        let mut stmt = prepared;
        // `LOCAL_SERIAL` keeps the Paxos round inside the local DC.
        // Global SERIAL is unsafe under multi-DC active-active, per
        // ADR-0020 / cassandra-kernel docs.
        stmt.set_serial_consistency(Some(SerialConsistency::LocalSerial));

        let result = self
            .session
            .execute(&stmt, (event_id, CqlTimestamp(now_ms)))
            .await
            .map_err(IdempotencyError::backend)?;

        // First column of an LWT response is `[applied]`.
        let applied = result
            .first_row()
            .ok()
            .and_then(|row| row.columns.into_iter().next().flatten())
            .and_then(|v| v.as_boolean())
            .unwrap_or(false);

        Ok(if applied {
            Outcome::FirstSeen
        } else {
            Outcome::AlreadyProcessed
        })
    }
}
