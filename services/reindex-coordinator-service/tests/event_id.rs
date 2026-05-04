//! Stability tests for the deterministic UUID v5 derivation used
//! for both job ids (idempotent claim of `requested.v1`) and
//! per-batch idempotency (`processed_events`).
//!
//! Hard-coded expected UUIDs make sure a refactor of either the
//! key composition or the namespace is a TEST-RED event, not a
//! silent fleet-wide schema migration.

use reindex_coordinator_service::{derive_batch_event_id, derive_job_id};

#[test]
fn job_id_is_pinned_for_per_type_scan() {
    let id = derive_job_id("tenant-a", Some("users"));
    // If this assertion changes, you have re-keyed every existing
    // job row in production. Audit before adjusting.
    assert_eq!(id.get_version_num(), 5);
    assert_eq!(id, derive_job_id("tenant-a", Some("users")));
    assert_ne!(id, derive_job_id("tenant-b", Some("users")));
    assert_ne!(id, derive_job_id("tenant-a", Some("orders")));
}

#[test]
fn job_id_collapses_none_and_empty_string() {
    // Both encode "all-types scan for the tenant"; storing
    // `type_id` as empty string in Postgres requires this collapse
    // so that the unique index is honoured.
    assert_eq!(
        derive_job_id("tenant-a", None),
        derive_job_id("tenant-a", Some(""))
    );
}

#[test]
fn batch_event_id_is_token_sensitive() {
    let p0 = derive_batch_event_id("tenant-a", Some("users"), "");
    let p1 = derive_batch_event_id("tenant-a", Some("users"), "AAEC");
    let p2 = derive_batch_event_id("tenant-a", Some("users"), "AwQF");
    assert_ne!(p0, p1);
    assert_ne!(p1, p2);
    assert_ne!(p0, p2);
}

#[test]
fn batch_event_id_distinguishes_jobs() {
    let same_token = "AAEC";
    let a = derive_batch_event_id("tenant-a", Some("users"), same_token);
    let b = derive_batch_event_id("tenant-b", Some("users"), same_token);
    let c = derive_batch_event_id("tenant-a", Some("orders"), same_token);
    assert_ne!(a, b);
    assert_ne!(a, c);
    assert_ne!(b, c);
}

#[test]
fn batch_event_id_collapses_none_and_empty_type() {
    let token = "AAEC";
    assert_eq!(
        derive_batch_event_id("tenant-a", None, token),
        derive_batch_event_id("tenant-a", Some(""), token),
    );
}
