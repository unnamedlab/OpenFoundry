//! JSON Schema validation for `ontology.reindex.v1` records.
//!
//! The schema text is checked into `services/ontology-indexer/schemas/`
//! and embedded into the binary via [`include_str!`] so the consumer
//! cannot diverge from the artifact published to Apicurio Registry
//! (the Helm chart `kafka-cluster` registers the same file —
//! see `infra/helm/infra/kafka-cluster/templates/apicurio-schemas-job.yaml`).
//!
//! The compiled validator is cached in a process-wide [`OnceLock`] so
//! the per-message hot path is `validator.is_valid(&value)` only
//! (no schema parse or compile work).
//!
//! Per Tarea 4.4 of the migration plan
//! (`docs/architecture/migration-plan-foundry-pattern-orchestration.md`),
//! validating the new producer's records against this schema is what
//! makes the cut-over from `workers-go/reindex` (removed in Tarea 4.3)
//! to `services/reindex-coordinator-service` (Tarea 4.2) safe.

use std::sync::OnceLock;

use jsonschema::JSONSchema;
use serde_json::Value;
use thiserror::Error;

/// Raw JSON Schema text for `ontology.reindex.v1` records.
///
/// Embedded so the validator and the artifact registered in Apicurio
/// stay byte-for-byte identical.
pub const REINDEX_V1_SCHEMA_JSON: &str = include_str!("../schemas/ontology.reindex.v1.json");

/// Apicurio / Confluent-compat subject name for `ontology.reindex.v1`
/// values. Pinned here so the consumer, the producer compat test, and
/// the Helm-time registration job cannot disagree on the subject.
pub const REINDEX_V1_SUBJECT: &str = "ontology.reindex.v1-value";

#[derive(Debug, Error)]
pub enum SchemaError {
    #[error("schema parse failed: {0}")]
    Parse(String),
    #[error("schema compile failed: {0}")]
    Compile(String),
    #[error("payload does not conform to ontology.reindex.v1: {0}")]
    Invalid(String),
}

fn validator() -> Result<&'static JSONSchema, SchemaError> {
    static VALIDATOR: OnceLock<JSONSchema> = OnceLock::new();
    if let Some(v) = VALIDATOR.get() {
        return Ok(v);
    }
    let value: Value = serde_json::from_str(REINDEX_V1_SCHEMA_JSON)
        .map_err(|e| SchemaError::Parse(e.to_string()))?;
    let compiled = JSONSchema::options()
        .with_draft(jsonschema::Draft::Draft7)
        .compile(&value)
        .map_err(|e| SchemaError::Compile(e.to_string()))?;
    // Tolerate a benign race: another thread may have populated it
    // between `get()` and `set()`. Either compiled validator is
    // identical for our purposes.
    let _ = VALIDATOR.set(compiled);
    Ok(VALIDATOR.get().expect("validator was just initialised"))
}

/// Eagerly compile the validator. Call once at process startup so
/// schema-parse errors fail loudly instead of on the first record.
pub fn ensure_compiled() -> Result<(), SchemaError> {
    validator().map(|_| ())
}

/// Validate a JSON-decoded payload against `ontology.reindex.v1`.
pub fn validate_value(payload: &Value) -> Result<(), SchemaError> {
    let v = validator()?;
    if let Err(errors) = v.validate(payload) {
        let msg: Vec<String> = errors.map(|e| e.to_string()).collect();
        return Err(SchemaError::Invalid(msg.join("; ")));
    }
    Ok(())
}

/// Validate raw Kafka bytes against `ontology.reindex.v1`. Returns
/// the parsed [`Value`] on success so callers do not pay the
/// `serde_json::from_slice` cost twice.
pub fn validate_bytes(bytes: &[u8]) -> Result<Value, SchemaError> {
    let value: Value = serde_json::from_slice(bytes)
        .map_err(|e| SchemaError::Invalid(format!("payload is not valid JSON: {e}")))?;
    validate_value(&value)?;
    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn sample() -> Value {
        json!({
            "tenant": "tenant-a",
            "id": "00000000-0000-0000-0000-000000000001",
            "type_id": "users",
            "version": 7,
            "payload": { "name": "alice" },
            "embedding": [0.1, 0.2, 0.3],
            "deleted": false
        })
    }

    #[test]
    fn embedded_schema_compiles() {
        ensure_compiled().expect("schema must compile at startup");
    }

    #[test]
    fn subject_name_is_pinned() {
        assert_eq!(REINDEX_V1_SUBJECT, "ontology.reindex.v1-value");
    }

    #[test]
    fn accepts_valid_record() {
        validate_value(&sample()).expect("valid record");
    }

    #[test]
    fn accepts_record_without_optional_fields() {
        let payload = json!({
            "tenant": "t",
            "id": "i",
            "type_id": "u",
            "version": 0,
            "payload": {}
        });
        validate_value(&payload).expect("optional fields are optional");
    }

    #[test]
    fn rejects_missing_required_field() {
        let mut payload = sample();
        payload.as_object_mut().unwrap().remove("tenant");
        let err = validate_value(&payload).expect_err("tenant is required");
        assert!(matches!(err, SchemaError::Invalid(_)));
    }

    #[test]
    fn rejects_negative_version() {
        let mut payload = sample();
        payload["version"] = json!(-1);
        let err = validate_value(&payload).expect_err("version >= 0");
        assert!(matches!(err, SchemaError::Invalid(_)));
    }

    #[test]
    fn rejects_wrong_type_for_payload() {
        let mut payload = sample();
        payload["payload"] = json!("not an object");
        let err = validate_value(&payload).expect_err("payload must be object");
        assert!(matches!(err, SchemaError::Invalid(_)));
    }

    #[test]
    fn rejects_non_numeric_embedding_entries() {
        let mut payload = sample();
        payload["embedding"] = json!(["nope"]);
        let err = validate_value(&payload).expect_err("embedding entries must be numbers");
        assert!(matches!(err, SchemaError::Invalid(_)));
    }

    #[test]
    fn validate_bytes_round_trip() {
        let bytes = serde_json::to_vec(&sample()).unwrap();
        let v = validate_bytes(&bytes).expect("valid bytes");
        assert_eq!(v["tenant"], "tenant-a");
    }

    #[test]
    fn validate_bytes_rejects_non_json() {
        let err = validate_bytes(b"not json").expect_err("must reject");
        assert!(matches!(err, SchemaError::Invalid(_)));
    }

    #[test]
    fn helm_chart_copy_is_in_sync_with_source() {
        // Drift guard for Tarea 4.4.
        //
        // The chart at `infra/helm/infra/kafka-cluster` ships a copy of
        // this schema under `files/schemas/` and registers it into
        // Apicurio Registry at install time
        // (`templates/apicurio-schemas-job.yaml`). The two copies MUST
        // be byte-identical so the consumer's compiled validator and
        // the registered Apicurio artifact agree on every field.
        const HELM_COPY: &str = include_str!(
            "../../../infra/helm/infra/kafka-cluster/files/schemas/ontology.reindex.v1.json"
        );
        assert_eq!(
            REINDEX_V1_SCHEMA_JSON, HELM_COPY,
            "ontology.reindex.v1.json drift: keep services/ontology-indexer/schemas/ \
             and infra/helm/infra/kafka-cluster/files/schemas/ in sync"
        );
    }
}
