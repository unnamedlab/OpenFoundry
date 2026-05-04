//! Tarea 4.4 — schema-level compatibility check between this
//! coordinator (the `ontology.reindex.v1` producer) and the
//! downstream consumer in `services/ontology-indexer`.
//!
//! The consumer compiles a JSON Schema at startup
//! (`services/ontology-indexer/src/schema.rs`) and rejects any
//! record on `ontology.reindex.v1` that does not conform. This
//! test loads **the same schema artifact** and validates that
//! [`reindex_coordinator_service::scan::encode_batch_record`]
//! produces records that pass it — both with and without the
//! optional `embedding` field — so the cut-over from
//! `workers-go/reindex` (removed in Tarea 4.3) cannot regress
//! the wire format silently.

use std::path::PathBuf;

use jsonschema::{Draft, JSONSchema};
use reindex_coordinator_service::scan::encode_batch_record;
use serde_json::{Value, json};

/// Path to the consumer-owned JSON Schema, resolved relative to
/// this crate's manifest dir so the test works under any cwd
/// (`cargo test -p ...`, `just test`, etc.).
fn load_schema() -> JSONSchema {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("ontology-indexer")
        .join("schemas")
        .join("ontology.reindex.v1.json");
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|err| panic!("read schema {}: {err}", path.display()));
    let value: Value =
        serde_json::from_str(&text).expect("ontology.reindex.v1 schema must be valid JSON");
    JSONSchema::options()
        .with_draft(Draft::Draft7)
        .compile(&value)
        .expect("ontology.reindex.v1 schema must compile under Draft-07")
}

fn assert_valid(schema: &JSONSchema, value: &Value) {
    if let Err(errors) = schema.validate(value) {
        let detail: Vec<String> = errors.map(|e| e.to_string()).collect();
        panic!(
            "record failed ontology.reindex.v1 validation: {}\n payload = {}",
            detail.join("; "),
            serde_json::to_string_pretty(value).unwrap_or_default()
        );
    }
}

#[test]
fn record_with_embedding_satisfies_consumer_schema() {
    let schema = load_schema();
    let record = encode_batch_record(
        "tenant-a".into(),
        "00000000-0000-0000-0000-000000000001".into(),
        "users".into(),
        7,
        json!({
            "name": "alice",
            "embedding": [0.1, 0.2, 0.3]
        }),
    );
    let value = serde_json::to_value(&record).expect("serialise record");
    assert_valid(&schema, &value);
}

#[test]
fn record_without_embedding_satisfies_consumer_schema() {
    let schema = load_schema();
    let record = encode_batch_record(
        "tenant-a".into(),
        "id-1".into(),
        "Aircraft".into(),
        0,
        json!({"tail_number": "EC-123"}),
    );
    let value = serde_json::to_value(&record).expect("serialise record");
    assert_valid(&schema, &value);
}
