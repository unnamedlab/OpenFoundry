//! Per-service helpers that derive the deterministic ids the saga
//! consumer relies on for idempotency.
//!
//! - `derive_saga_id(task_type, correlation_id)` — the
//!   `saga.state.saga_id` PK. Producer redeliveries that re-publish
//!   the same `(task_type, correlation_id)` pair collapse onto the
//!   same row.
//! - `derive_request_event_id(task_type, correlation_id)` — the
//!   `processed_events.event_id` used by the saga consumer's
//!   `idempotency::PgIdempotencyStore`. Distinct namespace from
//!   `derive_saga_id` so future code that needs to address the same
//!   request differently does not collide on a UUID.
//!
//! Both ids are UUIDv5 (RFC 4122 SHA-1 namespaced UUIDs) so they
//! depend purely on inputs. The namespace is hard-coded as a
//! `Uuid::from_bytes` constant generated once with `uuidgen`.

use uuid::Uuid;

/// Hard-coded UUIDv5 namespace for everything emitted by this
/// service. Generated once with `uuidgen` and pinned forever.
pub const AUTOMATION_OPS_NAMESPACE: Uuid = Uuid::from_bytes([
    0x83, 0xa1, 0x71, 0x2c, 0x4f, 0x9d, 0x49, 0x46, 0x9b, 0xc4, 0xb1, 0x6e, 0x6e, 0x57, 0x2c, 0x18,
]);

/// Derive the canonical `saga.state.saga_id` for a `(task_type,
/// correlation_id)` pair. Producer retries that re-publish the same
/// values collapse onto the same row.
pub fn derive_saga_id(task_type: &str, correlation_id: Uuid) -> Uuid {
    let mut buf = Vec::with_capacity(task_type.len() + 1 + 16);
    buf.extend_from_slice(task_type.as_bytes());
    buf.push(b'|');
    buf.extend_from_slice(correlation_id.as_bytes());
    Uuid::new_v5(&AUTOMATION_OPS_NAMESPACE, &buf)
}

/// Derive the per-request `event_id` for the `processed_events`
/// idempotency store. Defined separately from [`derive_saga_id`] so
/// the namespaces never collide if the saga id ever needs to be
/// addressable as a Kafka event id by another consumer.
pub fn derive_request_event_id(task_type: &str, correlation_id: Uuid) -> Uuid {
    let mut buf = Vec::with_capacity(task_type.len() + 2 + 16);
    buf.extend_from_slice(task_type.as_bytes());
    buf.push(b'|');
    buf.extend_from_slice(correlation_id.as_bytes());
    buf.push(b'R'); // distinguish from the saga-id namespace
    Uuid::new_v5(&AUTOMATION_OPS_NAMESPACE, &buf)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn derive_saga_id_is_stable() {
        let correlation = Uuid::nil();
        assert_eq!(
            derive_saga_id("retention.sweep", correlation),
            derive_saga_id("retention.sweep", correlation)
        );
    }

    #[test]
    fn derive_saga_id_differs_per_input() {
        let c1 = Uuid::now_v7();
        let c2 = Uuid::now_v7();
        assert_ne!(
            derive_saga_id("retention.sweep", c1),
            derive_saga_id("retention.sweep", c2)
        );
        assert_ne!(
            derive_saga_id("retention.sweep", c1),
            derive_saga_id("cleanup.workspace", c1)
        );
    }

    #[test]
    fn request_event_id_distinct_from_saga_id() {
        let correlation = Uuid::now_v7();
        assert_ne!(
            derive_saga_id("retention.sweep", correlation),
            derive_request_event_id("retention.sweep", correlation)
        );
    }

    #[test]
    fn ids_are_uuid_v5() {
        let id = derive_saga_id("retention.sweep", Uuid::nil());
        assert_eq!(id.get_version_num(), 5);
        let req = derive_request_event_id("retention.sweep", Uuid::nil());
        assert_eq!(req.get_version_num(), 5);
    }
}
