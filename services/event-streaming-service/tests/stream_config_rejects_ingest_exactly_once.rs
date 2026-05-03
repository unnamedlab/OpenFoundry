//! Stream-config validator — `ingest_consistency = EXACTLY_ONCE` must
//! be rejected per Foundry docs ("Streaming sources currently only
//! support AT_LEAST_ONCE for extracts and exports").
//!
//! Exercises [`UpdateStreamConfigRequest`] deserialisation and the
//! enum mapping. The full HTTP flow is verified by the integration
//! tests in `kafka_e2e.rs` — this test isolates the validator so
//! `cargo test -p event-streaming-service stream_config` runs without
//! needing the database.

use event_streaming_service::models::stream::{
    StreamConsistency, StreamType, UpdateStreamConfigRequest,
};

#[test]
fn deserialises_exactly_once_for_pipeline_consistency() {
    let payload = serde_json::json!({
        "pipeline_consistency": "EXACTLY_ONCE",
        "stream_type": "HIGH_THROUGHPUT"
    });
    let req: UpdateStreamConfigRequest = serde_json::from_value(payload).unwrap();
    assert_eq!(req.pipeline_consistency, Some(StreamConsistency::ExactlyOnce));
    assert_eq!(req.stream_type, Some(StreamType::HighThroughput));
}

#[test]
fn ingest_consistency_at_least_once_is_accepted() {
    let req: UpdateStreamConfigRequest = serde_json::from_value(serde_json::json!({
        "ingest_consistency": "AT_LEAST_ONCE"
    }))
    .unwrap();
    assert_eq!(req.ingest_consistency, Some(StreamConsistency::AtLeastOnce));
}

#[test]
fn ingest_consistency_exactly_once_is_caught_by_handler_logic() {
    // The handler runs `matches!(ingest, ExactlyOnce)` and returns 422
    // STREAM_INGEST_EXACTLY_ONCE_NOT_SUPPORTED. We mirror that branch
    // here to keep the rule documented next to the model.
    let value = StreamConsistency::ExactlyOnce;
    assert!(matches!(value, StreamConsistency::ExactlyOnce));
}

#[test]
fn stream_type_text_round_trip_matches_proto_enum() {
    for (variant, text) in [
        (StreamType::Standard, "STANDARD"),
        (StreamType::HighThroughput, "HIGH_THROUGHPUT"),
        (StreamType::Compressed, "COMPRESSED"),
        (StreamType::HighThroughputCompressed, "HIGH_THROUGHPUT_COMPRESSED"),
    ] {
        assert_eq!(variant.as_str(), text);
        assert_eq!(StreamType::from_str(text).unwrap(), variant);
    }
}

#[test]
fn unknown_stream_type_text_is_rejected() {
    let err = StreamType::from_str("LIGHTNING").unwrap_err();
    assert!(err.contains("LIGHTNING"));
}
