//! Reset stream — `force=true` overrides the downstream-active guard.
//!
//! Foundry docs: "Downstream consuming pipelines of the ingest stream
//! must be replayed after a reset." We refuse the reset by default
//! when at least one running topology references the stream and let
//! operators opt in by setting `force=true`. This test pins the
//! request shape and the documented error code.

use event_streaming_service::handlers::stream_views::ERR_RESET_DOWNSTREAM_ACTIVE;
use event_streaming_service::models::stream_view::ResetStreamRequest;

#[test]
fn error_code_matches_documented_constant() {
    assert_eq!(
        ERR_RESET_DOWNSTREAM_ACTIVE,
        "STREAM_RESET_DOWNSTREAM_PIPELINES_ACTIVE"
    );
}

#[test]
fn force_field_round_trips() {
    let body = serde_json::json!({ "force": true });
    let req: ResetStreamRequest = serde_json::from_value(body).unwrap();
    assert!(req.force);
}

#[test]
fn force_default_is_false() {
    let req: ResetStreamRequest = serde_json::from_value(serde_json::json!({})).unwrap();
    assert!(
        !req.force,
        "force must default to false so accidental resets do not bypass the downstream check"
    );
}

#[test]
fn new_schema_and_force_combine() {
    // The reset modal sends both fields when an operator updates the
    // schema during a forced reset; make sure deserialise picks up
    // both.
    let req: ResetStreamRequest = serde_json::from_value(serde_json::json!({
        "force": true,
        "new_schema": { "fields": [] }
    }))
    .unwrap();
    assert!(req.force);
    assert!(req.new_schema.is_some());
}
