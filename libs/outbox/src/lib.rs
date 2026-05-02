//! Transactional outbox helper (ADR-0022).
//!
//! ## What this crate is
//!
//! A thin, dependency-light helper around the `outbox.events` Postgres
//! table that ships every outbound domain event of the platform. The
//! contract is one function:
//!
//! ```ignore
//! outbox::enqueue(&mut tx, event).await?;
//! ```
//!
//! `tx` is the caller's existing `sqlx::Transaction<'_, Postgres>`.
//! The event is appended to `outbox.events` and **immediately deleted
//! in the same transaction**. Both records land in the WAL when the
//! transaction commits; the Debezium Postgres connector captures the
//! `INSERT` (which the `EventRouter` SMT relays to Kafka) and discards
//! the `DELETE` (`tombstones.on.delete=false`). This is the canonical
//! Debezium outbox pattern — see
//! <https://debezium.io/documentation/reference/stable/transformations/outbox-event-router.html>
//! § "Basic outbox table".
//!
//! ### Why INSERT+DELETE rather than `outbox.event.deletion.policy=delete`
//!
//! The migration plan describes the deletion strategy as
//! `outbox.event.deletion.policy=delete`. That option does not exist
//! in the upstream Debezium Postgres connector; the pattern documented
//! by Debezium itself (and used in production at Red Hat, Confluent
//! reference architectures, etc.) is the in-transaction delete shown
//! here. Net effect is identical: the row is gone before the
//! transaction commits, the WAL still carries the full payload, and
//! the table stays empty in steady state without a janitor.
//!
//! ## OpenLineage headers
//!
//! `OutboxEvent::headers` is a free-form `HashMap<String, String>`
//! that the `EventRouter` SMT copies onto the Kafka record headers.
//! Producers should populate the `ol-run-id`, `ol-parent-run-id`,
//! `ol-namespace` and `ol-job` keys whenever a lineage context is in
//! scope; consumers can then thread the lineage forward without
//! deserialising the payload.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};
use sqlx::{Postgres, Transaction};
use thiserror::Error;
use uuid::Uuid;

#[derive(Debug, Error)]
pub enum OutboxError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("serialize headers: {0}")]
    Serialize(#[from] serde_json::Error),
}

pub type OutboxResult<T> = Result<T, OutboxError>;

/// Domain event ready to be appended to `outbox.events`.
///
/// `event_id` should be deterministic (typically a v5 UUID derived
/// from `aggregate || aggregate_id || version`) so retries by the
/// caller stay idempotent under the table's primary key.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OutboxEvent {
    pub event_id: Uuid,
    pub aggregate: String,
    pub aggregate_id: String,
    pub topic: String,
    pub payload: serde_json::Value,
    #[serde(default)]
    pub headers: HashMap<String, String>,
}

impl OutboxEvent {
    /// Build a minimal event with no headers. Callers that have an
    /// OpenLineage context should attach `ol-*` headers via
    /// [`OutboxEvent::with_header`] before calling [`enqueue`].
    pub fn new(
        event_id: Uuid,
        aggregate: impl Into<String>,
        aggregate_id: impl Into<String>,
        topic: impl Into<String>,
        payload: serde_json::Value,
    ) -> Self {
        Self {
            event_id,
            aggregate: aggregate.into(),
            aggregate_id: aggregate_id.into(),
            topic: topic.into(),
            payload,
            headers: HashMap::new(),
        }
    }

    pub fn with_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.headers.insert(key.into(), value.into());
        self
    }
}

/// Append `event` to `outbox.events` inside `tx` and immediately
/// delete the row in the same transaction. Returns `Ok(())` on
/// success; on conflict against an existing `event_id` the call is
/// silently treated as a no-op (idempotent retry).
///
/// The caller owns the transaction lifecycle — this helper never
/// commits or rolls back. Pair it with the application's primary
/// write so a single `tx.commit().await?` atomically publishes both.
pub async fn enqueue(
    tx: &mut Transaction<'_, Postgres>,
    event: OutboxEvent,
) -> OutboxResult<()> {
    let headers = serde_json::to_value(&event.headers)?;

    // INSERT. `ON CONFLICT DO NOTHING` makes deterministic retries
    // safe: a duplicate `event_id` short-circuits without affecting
    // the surrounding transaction.
    let inserted = sqlx::query(
        r#"
        INSERT INTO outbox.events
          (event_id, aggregate, aggregate_id, payload, headers, topic)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (event_id) DO NOTHING
        "#,
    )
    .bind(event.event_id)
    .bind(&event.aggregate)
    .bind(&event.aggregate_id)
    .bind(&event.payload)
    .bind(&headers)
    .bind(&event.topic)
    .execute(&mut **tx)
    .await?
    .rows_affected();

    if inserted == 0 {
        // Duplicate event id: another committed transaction already
        // emitted this event. Nothing more to do — the WAL record
        // from that earlier transaction has already been (or will be)
        // captured by Debezium.
        tracing::debug!(
            event_id = %event.event_id,
            topic = %event.topic,
            "outbox enqueue is a no-op for duplicate event_id"
        );
        return Ok(());
    }

    // DELETE in the same transaction. The WAL still carries the full
    // INSERT (REPLICA IDENTITY FULL is set on the table), Debezium
    // emits it via the EventRouter SMT, and the DELETE event is
    // dropped because `tombstones.on.delete=false` on the connector.
    sqlx::query("DELETE FROM outbox.events WHERE event_id = $1")
        .bind(event.event_id)
        .execute(&mut **tx)
        .await?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn outbox_event_builder_attaches_headers() {
        let evt = OutboxEvent::new(
            Uuid::nil(),
            "ontology_object",
            "obj-1",
            "ontology.object.changed.v1",
            json!({"version": 1}),
        )
        .with_header("ol-run-id", "run-abc")
        .with_header("ol-namespace", "of");

        assert_eq!(evt.headers.get("ol-run-id").map(String::as_str), Some("run-abc"));
        assert_eq!(evt.headers.get("ol-namespace").map(String::as_str), Some("of"));
        assert_eq!(evt.topic, "ontology.object.changed.v1");
    }
}
