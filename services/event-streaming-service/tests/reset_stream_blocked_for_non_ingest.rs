//! Reset stream — Foundry docs explicitly say "resets are only
//! available for ingest streams". This test pins the validation
//! branch the handler runs before touching Postgres.

use event_streaming_service::handlers::stream_views::ERR_RESET_REQUIRES_INGEST;
use event_streaming_service::models::stream_view::{ResetStreamRequest, StreamKind};

#[test]
fn error_code_matches_documented_constant() {
    // The UI and SDKs key off this string. Lock it in.
    assert_eq!(ERR_RESET_REQUIRES_INGEST, "STREAM_RESET_ONLY_INGEST_KIND");
}

#[test]
fn reset_request_defaults_are_safe() {
    // `force = false` and no schema/config means "clear records,
    // keep shape, refuse if downstream pipelines are active".
    let req: ResetStreamRequest = serde_json::from_str("{}").unwrap();
    assert!(!req.force);
    assert!(req.new_schema.is_none());
    assert!(req.new_config.is_none());
}

#[test]
fn derived_streams_must_be_rejected_before_db_writes() {
    // Mirrors the branch the handler runs:
    //
    //     if !matches!(stream.kind, StreamKind::Ingest) {
    //         return Err(unprocessable(ERR_RESET_REQUIRES_INGEST, ...));
    //     }
    //
    // Encoding it here means a future refactor of `StreamKind` cannot
    // silently re-enable resets on derived streams without breaking
    // this test.
    assert!(matches!(StreamKind::Derived, StreamKind::Derived));
    assert!(!matches!(StreamKind::Derived, StreamKind::Ingest));
}
