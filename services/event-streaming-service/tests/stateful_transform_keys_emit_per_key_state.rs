//! Bloque P6 — stateful transforms emit per-key state.
//!
//! Mirrors the Foundry "Streaming keys" + "Stateful transforms" docs:
//! when a window is keyed, the runtime maintains an independent state
//! slice per unique value of `key_columns`. The
//! [`event_streaming_service::models::window::key_prefix_for`] helper
//! is the deterministic part of that contract.

use chrono::Utc;
use event_streaming_service::models::window::{WindowDefinition, key_prefix_for};
use serde_json::json;
use uuid::Uuid;

fn build(keyed: bool, columns: &[&str], ttl: i32) -> WindowDefinition {
    WindowDefinition {
        id: Uuid::nil(),
        name: "w".into(),
        description: String::new(),
        status: "active".into(),
        window_type: "tumbling".into(),
        duration_seconds: 60,
        slide_seconds: 60,
        session_gap_seconds: 0,
        allowed_lateness_seconds: 0,
        aggregation_keys: vec![],
        measure_fields: vec![],
        keyed,
        key_columns: columns.iter().map(|s| s.to_string()).collect(),
        state_ttl_seconds: ttl,
        created_at: Utc::now(),
        updated_at: Utc::now(),
    }
}

#[test]
fn keyed_window_partitions_records_into_distinct_state_slices() {
    let w = build(true, &["customer_id"], 3600);
    let a = json!({"customer_id": "c-1", "amount": 10});
    let b = json!({"customer_id": "c-2", "amount": 20});
    let c = json!({"customer_id": "c-1", "amount": 30});
    let ka = key_prefix_for(&w, &a).unwrap();
    let kb = key_prefix_for(&w, &b).unwrap();
    let kc = key_prefix_for(&w, &c).unwrap();
    assert_eq!(ka, "c-1");
    assert_eq!(kb, "c-2");
    assert_eq!(ka, kc, "same key produces the same state slice");
    assert_ne!(ka, kb);
}

#[test]
fn keyed_window_with_multiple_columns_concatenates_with_pipe() {
    let w = build(true, &["customer_id", "country"], 0);
    let r = json!({"customer_id": "c-1", "country": "US"});
    assert_eq!(key_prefix_for(&w, &r), Some("c-1|US".to_string()));
}

#[test]
fn unkeyed_window_returns_none() {
    let w = build(false, &[], 0);
    assert_eq!(key_prefix_for(&w, &json!({"customer_id": "c-1"})), None);
}

#[test]
fn missing_key_column_yields_empty_segment() {
    // Operators sometimes mark a column as nullable but use it as a
    // key. The deterministic resolver must not panic; missing fields
    // produce an empty segment so two records with different missing
    // columns still hash distinctly when other key columns differ.
    let w = build(true, &["customer_id", "country"], 0);
    let r1 = json!({"customer_id": "c-1"});
    let r2 = json!({"country": "US"});
    let k1 = key_prefix_for(&w, &r1).unwrap();
    let k2 = key_prefix_for(&w, &r2).unwrap();
    assert_eq!(k1, "c-1|");
    assert_eq!(k2, "|US");
    assert_ne!(k1, k2);
}

#[test]
fn state_ttl_seconds_round_trip_through_json() {
    let w = build(true, &["customer_id"], 7200);
    let value = serde_json::to_value(&w).unwrap();
    assert_eq!(value["state_ttl_seconds"], 7200);
    assert_eq!(value["keyed"], true);
    assert_eq!(value["key_columns"][0], "customer_id");
}
