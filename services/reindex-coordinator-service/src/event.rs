//! Wire format of the Kafka events the coordinator consumes and
//! produces, plus the deterministic `event_id` derivation used for
//! per-job and per-batch idempotency.
//!
//! The derivation uses **UUID v5** (SHA-1 namespaced UUIDs, RFC
//! 4122) so the resulting id depends only on the inputs — replays
//! of the same `(tenant_id, type_id, page_token)` always produce
//! the same id, which is what makes the `idempotency` Postgres
//! table do the right thing on a redelivery after a crash.

use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// UUID-v5 namespace for everything emitted by this service.
///
/// Hard-coded constant rather than `Uuid::NAMESPACE_OID` so a
/// future namespace migration is a single-line change instead of a
/// fleet-wide schema dance. Generated once with `uuidgen` and
/// pinned forever.
pub const REINDEX_NAMESPACE: Uuid = Uuid::from_bytes([
    0x6f, 0x82, 0x4d, 0x6e, 0x71, 0xa1, 0x4b, 0x9b, 0x9c, 0xfe, 0x9f, 0x4f, 0x07, 0x2c, 0x88, 0x10,
]);

/// Payload of `ontology.reindex.requested.v1`.
///
/// **The producer does NOT supply a resume token.** The coordinator
/// owns the cursor in `pg-runtime-config.reindex_jobs` (see
/// `docs/architecture/refactor/reindex-worker-inventory.md` §6),
/// so a second `requested` event for the same `(tenant, type)` is
/// either a no-op (job still running) or a re-start of a finished
/// job (transition validated by the state machine).
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ReindexRequestedV1 {
    pub tenant_id: String,
    /// Optional. Empty / absent ⇒ scan all types via the
    /// `ALLOW FILTERING` path.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub type_id: Option<String>,
    /// Optional override. Coordinator clamps to `1..=10_000` and
    /// defaults to `1000` to match the Go worker
    /// (`workers-go/reindex/activities/activities.go::scanInput::PageSize`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub page_size: Option<i32>,
    /// Optional opaque correlation id surfaced by the producer.
    /// Carried through to the `completed.v1` event so the caller
    /// can stitch request → completion together without leaning
    /// on Kafka offsets.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
}

/// Payload of `ontology.reindex.completed.v1`. Mirrors the
/// `OntologyReindexResult` struct of the legacy Go workflow
/// (`workers-go/reindex/internal/contract/contract.go::OntologyReindexResult`)
/// so downstream observability dashboards do not have to special-
/// case the cut-over.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ReindexCompletedV1 {
    pub job_id: Uuid,
    pub tenant_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub type_id: Option<String>,
    pub scanned: i64,
    pub published: i64,
    /// One of `completed` / `failed` / `cancelled`. Validated by
    /// [`crate::state::JobStatus::is_terminal`] before being written
    /// here.
    pub status: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
}

/// Derive the stable job UUID from `(tenant_id, type_id?)`.
///
/// `(tenant_id, "")` (no type) and `(tenant_id, "users")` are
/// **distinct** jobs by design — the all-types scan is its own
/// job, separate from per-type scans, mirroring the Go worker's
/// two scan paths.
///
/// Using UUID v5 here is what makes the
/// `INSERT … ON CONFLICT DO NOTHING` in [`crate::state`] the
/// canonical "claim or join" primitive: a second `requested.v1`
/// for the same key resolves to the same row.
pub fn derive_job_id(tenant_id: &str, type_id: Option<&str>) -> Uuid {
    let mut buf = String::with_capacity(tenant_id.len() + 1 + type_id.map_or(0, str::len));
    buf.push_str(tenant_id);
    buf.push('|');
    buf.push_str(type_id.unwrap_or(""));
    Uuid::new_v5(&REINDEX_NAMESPACE, buf.as_bytes())
}

/// Derive the per-batch idempotency `event_id` from
/// `(tenant_id, type_id?, page_token)`.
///
/// `page_token` is the opaque base64 of the gocql `PageState`
/// returned by the previous Cassandra page; the empty string is
/// used for the first page so that "page 0" of a job has its own
/// stable id distinct from "page 1".
///
/// Recorded in `reindex_coordinator.processed_events` BEFORE the
/// batch is produced, per the record-before-process rule of
/// `libs/idempotency` (see ADR-0038). On a crash between produce
/// and `resume_token` update, the next attempt sees
/// `Outcome::AlreadyProcessed` and skips re-publishing — the
/// downstream indexer is also idempotent on `(tenant, id, version)`
/// so the only effect is that the row update catches up.
pub fn derive_batch_event_id(tenant_id: &str, type_id: Option<&str>, page_token: &str) -> Uuid {
    let mut buf = String::with_capacity(tenant_id.len() + 2 + page_token.len());
    buf.push_str(tenant_id);
    buf.push('|');
    buf.push_str(type_id.unwrap_or(""));
    buf.push('|');
    buf.push_str(page_token);
    Uuid::new_v5(&REINDEX_NAMESPACE, buf.as_bytes())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn job_id_is_stable_across_calls() {
        let a = derive_job_id("tenant-a", Some("users"));
        let b = derive_job_id("tenant-a", Some("users"));
        assert_eq!(a, b);
        assert_eq!(a.get_version_num(), 5);
    }

    #[test]
    fn job_id_distinguishes_all_types_from_per_type() {
        let all = derive_job_id("tenant-a", None);
        let typed = derive_job_id("tenant-a", Some(""));
        let users = derive_job_id("tenant-a", Some("users"));
        // None and Some("") collapse to the same key by design —
        // the all-types job for a tenant is one row, regardless of
        // whether the producer sent the field as null or as "".
        assert_eq!(all, typed);
        assert_ne!(all, users);
    }

    #[test]
    fn job_id_distinguishes_tenants() {
        let a = derive_job_id("tenant-a", Some("users"));
        let b = derive_job_id("tenant-b", Some("users"));
        assert_ne!(a, b);
    }

    #[test]
    fn batch_event_id_is_stable_and_token_sensitive() {
        let p0 = derive_batch_event_id("tenant-a", Some("users"), "");
        let p0b = derive_batch_event_id("tenant-a", Some("users"), "");
        let p1 = derive_batch_event_id("tenant-a", Some("users"), "AAECAw==");
        assert_eq!(p0, p0b);
        assert_ne!(p0, p1);
        assert_eq!(p0.get_version_num(), 5);
    }

    #[test]
    fn requested_v1_round_trip() {
        let r = ReindexRequestedV1 {
            tenant_id: "tenant-a".into(),
            type_id: Some("users".into()),
            page_size: Some(500),
            request_id: Some("req-1".into()),
        };
        let json = serde_json::to_string(&r).unwrap();
        let back: ReindexRequestedV1 = serde_json::from_str(&json).unwrap();
        assert_eq!(r, back);
    }

    #[test]
    fn requested_v1_omits_optional_fields() {
        let r = ReindexRequestedV1 {
            tenant_id: "tenant-a".into(),
            type_id: None,
            page_size: None,
            request_id: None,
        };
        let json = serde_json::to_string(&r).unwrap();
        // Should not contain the optional keys at all.
        assert_eq!(json, r#"{"tenant_id":"tenant-a"}"#);
    }

    #[test]
    fn completed_v1_round_trip() {
        let c = ReindexCompletedV1 {
            job_id: derive_job_id("tenant-a", Some("users")),
            tenant_id: "tenant-a".into(),
            type_id: Some("users".into()),
            scanned: 12345,
            published: 12000,
            status: "completed".into(),
            error: None,
            request_id: None,
        };
        let json = serde_json::to_string(&c).unwrap();
        let back: ReindexCompletedV1 = serde_json::from_str(&json).unwrap();
        assert_eq!(c, back);
    }
}
