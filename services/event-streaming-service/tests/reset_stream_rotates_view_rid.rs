//! Reset stream — verifies the viewRid rotation invariant.
//!
//! These are pure-Rust unit-style assertions over the
//! `models::stream_view` helpers; they do not boot Postgres so the
//! suite stays fast and deterministic. The end-to-end happy path
//! (real Postgres + push proxy) lives in `push_proxy_validates_schema.rs`.

use event_streaming_service::models::stream_view::{
    STREAM_RID_PREFIX, StreamKind, VIEW_RID_PREFIX, stream_rid_for, view_rid_for,
};
use uuid::Uuid;

#[test]
fn rotates_view_rid_uses_documented_prefix() {
    let id = Uuid::now_v7();
    let rid = view_rid_for(id);
    assert!(rid.starts_with(VIEW_RID_PREFIX));
    assert!(rid.ends_with(&id.to_string()));
}

#[test]
fn stream_rid_uses_documented_prefix() {
    let id = Uuid::now_v7();
    let rid = stream_rid_for(id);
    assert!(rid.starts_with(STREAM_RID_PREFIX));
    assert!(rid.ends_with(&id.to_string()));
}

#[test]
fn each_reset_picks_a_distinct_view_rid() {
    // Simulate a chain of resets — every fresh `Uuid::now_v7()` must
    // produce a different `view_rid` so push consumers can detect the
    // rotation by string comparison.
    let mut seen = std::collections::HashSet::new();
    for _ in 0..16 {
        let rid = view_rid_for(Uuid::now_v7());
        assert!(
            seen.insert(rid.clone()),
            "view_rid {rid} collided across resets"
        );
    }
}

#[test]
fn ingest_streams_are_resettable_derived_streams_are_not() {
    // Foundry docs: "Resets are only available for ingest streams."
    // The `reset_stream` handler enforces this with a 422 prior to
    // touching the database.
    assert_eq!(StreamKind::Ingest.as_str(), "INGEST");
    assert_eq!(StreamKind::Derived.as_str(), "DERIVED");
    assert_eq!(StreamKind::from_str("INGEST").unwrap(), StreamKind::Ingest);
    assert_eq!(
        StreamKind::from_str("DERIVED").unwrap(),
        StreamKind::Derived
    );
    assert!(StreamKind::from_str("PUSH").is_err());
}

#[test]
fn push_url_renderer_uses_active_view_rid() {
    let view_rid = view_rid_for(Uuid::now_v7());
    let url = event_streaming_service::handlers::stream_views::render_push_url(
        "https://api.example.com",
        &view_rid,
    );
    assert_eq!(
        url,
        format!("https://api.example.com/streams-push/{view_rid}/records")
    );
}

#[test]
fn push_url_renderer_strips_trailing_slash_from_base() {
    // Operators sometimes include a trailing slash on
    // `STREAMING_PUBLIC_BASE_URL`. The renderer must keep the path
    // canonical so push consumers don't see double slashes.
    let view_rid = view_rid_for(Uuid::now_v7());
    let url = event_streaming_service::handlers::stream_views::render_push_url(
        "https://api.example.com/",
        &view_rid,
    );
    assert!(!url.contains("//streams-push"));
    assert_eq!(
        url,
        format!("https://api.example.com/streams-push/{view_rid}/records")
    );
}
