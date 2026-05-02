//! Writeback substrate for S1.4 (`ontology-actions-service`).
//!
//! Implements the canonical write pattern described in the migration
//! plan §S1.4.c:
//!
//! 1. Compute a deterministic [`event_id`](OutboxEvent::event_id) so
//!    retries collapse to the same row across both stores.
//! 2. Issue the primary write to Cassandra with optimistic
//!    concurrency. If Cassandra fails ⇒ the caller has nothing to
//!    rollback (no PG transaction was opened); the helper returns an
//!    error.
//! 3. Open a PG transaction against `pg-policy`.
//! 4. Append the domain event with [`outbox::enqueue`].
//! 5. `COMMIT`. If the commit fails *after* Cassandra succeeded, the
//!    helper surfaces a [`WritebackError::CommitAfterPrimary`] that
//!    carries `event_id` and `committed_version`, so the caller can
//!    retry the **same** call with the **same** input — the
//!    Cassandra write is then idempotent (LWT conflict whose
//!    `actual_version` matches the version we just tried to write is
//!    treated as success), and the outbox `INSERT … ON CONFLICT DO
//!    NOTHING` collapses the duplicate enqueue.
//!
//! The helper is intentionally narrow: it speaks `&dyn ObjectStore`
//! and `&PgPool`, so it is reusable by every ontology service that
//! migrates to the trait substrate, not just `ontology-actions-service`.
//!
//! ## Determinism of `event_id`
//!
//! `event_id` is a UUID-v5 over the namespace `ONTOLOGY_NAMESPACE`
//! and the canonical name `"{tenant}/{aggregate}/{aggregate_id}@{version}"`.
//! Two callers that try to commit the same `(tenant, aggregate,
//! aggregate_id, version)` triple will produce the same `event_id`,
//! making both Cassandra (LWT keyed by `(tenant, object_id)` +
//! `revision_number`) and Postgres (`outbox.events.event_id` PK)
//! idempotent for the *exact same* logical write.

use std::time::Duration;

use outbox::{OutboxError, OutboxEvent};
use serde_json::Value;
use sqlx::PgPool;
use storage_abstraction::repositories::{Object, ObjectStore, PutOutcome, RepoError};
use thiserror::Error;
use tracing::warn;
use uuid::Uuid;

/// UUID-v5 namespace for ontology writeback events. The literal value
/// is stable across releases (`uuid::Uuid::new_v5(&Uuid::NAMESPACE_URL,
/// b"https://openfoundry.dev/ns/ontology/writeback")`) — pinned here so
/// the `event_id` derivation never depends on runtime URL parsing.
pub const ONTOLOGY_NAMESPACE: Uuid = Uuid::from_bytes([
    0x4a, 0x52, 0x0f, 0x9a, 0x6c, 0x9d, 0x5b, 0x18, 0x9d, 0x4c, 0x88, 0x6f, 0x60, 0x9b, 0x16, 0x05,
]);

/// Errors raised by [`apply_object_with_outbox`].
#[derive(Debug, Error)]
pub enum WritebackError {
    /// The Cassandra write failed before any PG transaction was opened.
    #[error("primary store rejected the write: {0}")]
    Primary(#[from] RepoError),
    /// The PG transaction (or commit) failed *after* the Cassandra
    /// write succeeded. Carries the `event_id` and `committed_version`
    /// so the caller can build a deterministic retry that lands on the
    /// same idempotent identity.
    #[error(
        "commit after primary write failed (event_id={event_id}, version={committed_version}): {source}"
    )]
    CommitAfterPrimary {
        event_id: Uuid,
        committed_version: u64,
        #[source]
        source: OutboxError,
    },
    /// The PG transaction failed before the outbox row was even
    /// inserted (i.e. opening the transaction failed). Same retry
    /// semantics as `CommitAfterPrimary`.
    #[error(
        "could not open pg-policy tx after primary write (event_id={event_id}, version={committed_version}): {source}"
    )]
    OpenTxAfterPrimary {
        event_id: Uuid,
        committed_version: u64,
        #[source]
        source: sqlx::Error,
    },
    /// The Cassandra write returned a real version conflict (i.e. the
    /// stored version is *not* the version we just tried to write).
    /// The caller should refresh and re-base before retrying.
    #[error("version conflict: expected {expected_version}, found {actual_version}")]
    VersionConflict {
        expected_version: u64,
        actual_version: u64,
    },
}

/// Result of a successful writeback.
#[derive(Debug, Clone)]
pub struct WritebackOutcome {
    /// Deterministic event id used both as Cassandra-idempotency
    /// breadcrumb and as the outbox row PK.
    pub event_id: Uuid,
    /// Version that landed in Cassandra.
    pub committed_version: u64,
    /// `true` when the row was created in this call; `false` when an
    /// existing row was updated.
    pub created: bool,
    /// `true` when the helper detected a successful retry of a prior
    /// attempt (Cassandra LWT conflict whose `actual_version` matched
    /// the target version). The outbox enqueue still runs because it
    /// is itself idempotent.
    pub idempotent_retry: bool,
}

/// Apply a write to Cassandra and publish the resulting domain event
/// through the Postgres outbox in a single, retry-safe sequence. See
/// the module-level docs for the full state machine.
///
/// `aggregate` is the logical bucket name (e.g. `"object"` or
/// `"link"`); `topic` is the Kafka topic the Debezium router will
/// publish to (e.g. `"ontology.object.changed.v1"`); `payload` is the
/// caller-supplied JSON body of the event.
pub async fn apply_object_with_outbox(
    pg: &PgPool,
    objects: &dyn ObjectStore,
    object: Object,
    expected_version: Option<u64>,
    aggregate: &str,
    topic: &str,
    payload: Value,
) -> Result<WritebackOutcome, WritebackError> {
    let target_version = expected_version.map(|v| v + 1).unwrap_or(1);
    let event_id = derive_event_id(&object.tenant.0, aggregate, &object.id.0, target_version);

    // -- 1. Primary write -------------------------------------------------
    let outcome = objects.put(object.clone(), expected_version).await?;
    let (committed_version, created, idempotent_retry) = match outcome {
        PutOutcome::Inserted => (1u64, true, false),
        PutOutcome::Updated { new_version, .. } => (new_version, false, false),
        PutOutcome::VersionConflict {
            expected_version: e,
            actual_version,
        } if actual_version == target_version => {
            // Cassandra already accepted an identical prior attempt.
            // Treat as success and let the outbox enqueue collapse via
            // its own `ON CONFLICT DO NOTHING`.
            warn!(
                tenant = %object.tenant.0,
                object_id = %object.id.0,
                expected = e,
                actual = actual_version,
                "writeback: idempotent retry — cassandra already at target version"
            );
            (actual_version, expected_version.is_none(), true)
        }
        PutOutcome::VersionConflict {
            expected_version: e,
            actual_version,
        } => {
            return Err(WritebackError::VersionConflict {
                expected_version: e,
                actual_version,
            });
        }
    };

    // -- 2. Outbox enqueue inside a pg-policy transaction ----------------
    let event = OutboxEvent::new(event_id, aggregate, &object.id.0, topic, payload);

    let mut tx = pg
        .begin()
        .await
        .map_err(|source| WritebackError::OpenTxAfterPrimary {
            event_id,
            committed_version,
            source,
        })?;

    if let Err(source) = outbox::enqueue(&mut tx, event).await {
        // Surface the failure with full retry context. The helper does
        // not roll back the Cassandra write — the caller retries the
        // entire call with the same input (idempotent in both stores).
        return Err(WritebackError::CommitAfterPrimary {
            event_id,
            committed_version,
            source,
        });
    }

    if let Err(source) = tx.commit().await {
        return Err(WritebackError::CommitAfterPrimary {
            event_id,
            committed_version,
            source: OutboxError::Db(source),
        });
    }

    Ok(WritebackOutcome {
        event_id,
        committed_version,
        created,
        idempotent_retry,
    })
}

/// Derive the deterministic UUID-v5 event id from the
/// `(tenant, aggregate, aggregate_id, version)` quadruple. Exposed so
/// callers can pre-compute the id for tracing or testing.
pub fn derive_event_id(tenant: &str, aggregate: &str, aggregate_id: &str, version: u64) -> Uuid {
    let name = format!("{tenant}/{aggregate}/{aggregate_id}@{version}");
    Uuid::new_v5(&ONTOLOGY_NAMESPACE, name.as_bytes())
}

/// Reasonable default per-call budget for the writeback helper.
/// Provided as a constant rather than a parameter because the helper
/// itself does not enforce it — callers wrap the call in
/// `tokio::time::timeout(WRITEBACK_BUDGET, …)` when they need a hard
/// SLO. Pinned at 2 s so it composes with the 5 ms / 20 ms / 50 ms
/// SLO targets in S1.8 without ever becoming the bottleneck.
pub const WRITEBACK_BUDGET: Duration = Duration::from_secs(2);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn event_id_is_deterministic() {
        let a = derive_event_id("acme", "object", "obj-123", 7);
        let b = derive_event_id("acme", "object", "obj-123", 7);
        assert_eq!(a, b);
    }

    #[test]
    fn event_id_differs_when_any_field_changes() {
        let base = derive_event_id("acme", "object", "obj-123", 7);
        assert_ne!(base, derive_event_id("other", "object", "obj-123", 7));
        assert_ne!(base, derive_event_id("acme", "link", "obj-123", 7));
        assert_ne!(base, derive_event_id("acme", "object", "obj-124", 7));
        assert_ne!(base, derive_event_id("acme", "object", "obj-123", 8));
    }

    #[test]
    fn event_id_uses_v5_namespace() {
        let id = derive_event_id("acme", "object", "obj-123", 1);
        assert_eq!(id.get_version_num(), 5);
    }
}
