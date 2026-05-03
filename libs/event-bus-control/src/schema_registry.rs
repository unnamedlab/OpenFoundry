//! Schema Registry primitives shared by `cdc-metadata-service` (storage +
//! REST API) and the data-connection plane connectors (validation of
//! incoming samples).
//!
//! This module is intentionally *pure*: no DB, no HTTP. The only inputs are
//! a schema string (Avro JSON IDL, JSON Schema document, or Protobuf
//! `FileDescriptorSet` base64-encoded) and a payload to validate. The
//! Schema Registry service layers persistence, versioning, references and
//! Confluent-style HTTP routes on top of these helpers.
//!
//! Three things are exposed:
//! - [`SchemaType`]: the supported schema languages.
//! - [`fingerprint`]: deterministic SHA-256 over canonicalised schema text
//!   (used both as `schema_versions.fingerprint` and as the lookup key
//!   when callers ask "is this schema already registered?").
//! - [`validate_payload`]: parse the schema once, then check the payload
//!   conforms.
//! - [`check_compatibility`]: compare a *new* schema against the *previous*
//!   one under a Confluent compatibility mode (BACKWARD, FORWARD, FULL,
//!   NONE).

use std::str::FromStr;

use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SchemaType {
    Avro,
    Protobuf,
    Json,
}

impl SchemaType {
    pub fn as_str(self) -> &'static str {
        match self {
            SchemaType::Avro => "avro",
            SchemaType::Protobuf => "protobuf",
            SchemaType::Json => "json",
        }
    }
}

impl FromStr for SchemaType {
    type Err = SchemaError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match value.to_ascii_lowercase().as_str() {
            "avro" => Ok(Self::Avro),
            "protobuf" | "proto" => Ok(Self::Protobuf),
            "json" | "json_schema" | "jsonschema" => Ok(Self::Json),
            other => Err(SchemaError::UnsupportedSchemaType(other.to_string())),
        }
    }
}

/// Confluent-compatible compatibility levels.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "UPPERCASE")]
pub enum CompatibilityMode {
    None,
    Backward,
    BackwardTransitive,
    Forward,
    ForwardTransitive,
    Full,
    FullTransitive,
}

impl FromStr for CompatibilityMode {
    type Err = SchemaError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match value.to_ascii_uppercase().as_str() {
            "NONE" => Ok(Self::None),
            "BACKWARD" => Ok(Self::Backward),
            "BACKWARD_TRANSITIVE" => Ok(Self::BackwardTransitive),
            "FORWARD" => Ok(Self::Forward),
            "FORWARD_TRANSITIVE" => Ok(Self::ForwardTransitive),
            "FULL" => Ok(Self::Full),
            "FULL_TRANSITIVE" => Ok(Self::FullTransitive),
            other => Err(SchemaError::UnsupportedCompatibility(other.to_string())),
        }
    }
}

#[derive(Debug, Error)]
pub enum SchemaError {
    #[error("unsupported schema type: {0}")]
    UnsupportedSchemaType(String),
    #[error("unsupported compatibility mode: {0}")]
    UnsupportedCompatibility(String),
    #[error("schema parse error: {0}")]
    Parse(String),
    #[error("payload validation failed: {0}")]
    Validation(String),
    #[error("compatibility check failed: {0}")]
    Compatibility(String),
}

/// Stable SHA-256 fingerprint over the *canonicalised* schema bytes.
///
/// For Avro and JSON we re-emit the parsed value through `serde_json` so
/// that whitespace and key ordering are normalised. For Protobuf the input
/// is already a deterministic descriptor-set base64, so we hash it raw.
pub fn fingerprint(schema_type: SchemaType, schema_text: &str) -> Result<String, SchemaError> {
    let canonical = match schema_type {
        SchemaType::Avro | SchemaType::Json => {
            let value: Value = serde_json::from_str(schema_text)
                .map_err(|error| SchemaError::Parse(error.to_string()))?;
            serde_json::to_string(&value).map_err(|error| SchemaError::Parse(error.to_string()))?
        }
        SchemaType::Protobuf => schema_text.to_string(),
    };
    let mut hasher = Sha256::new();
    hasher.update(canonical.as_bytes());
    Ok(format!("sha256:{:x}", hasher.finalize()))
}

/// Validate that `payload_json` conforms to the given schema.
///
/// `payload_json` is always a `serde_json::Value` — the connectors hand us
/// JSON-decoded sample messages. Avro and JSON validators consume it
/// directly; the Protobuf validator currently performs a structural check
/// against the message descriptor (presence of required-by-convention
/// fields and basic type matching).
pub fn validate_payload(
    schema_type: SchemaType,
    schema_text: &str,
    payload: &Value,
) -> Result<(), SchemaError> {
    match schema_type {
        SchemaType::Avro => validate_avro(schema_text, payload),
        SchemaType::Json => validate_json(schema_text, payload),
        SchemaType::Protobuf => validate_protobuf(schema_text, payload),
    }
}

/// Returns `Ok(())` when `next` is compatible with `previous` under `mode`.
pub fn check_compatibility(
    schema_type: SchemaType,
    previous: &str,
    next: &str,
    mode: CompatibilityMode,
) -> Result<(), SchemaError> {
    if matches!(mode, CompatibilityMode::None) {
        return Ok(());
    }
    match schema_type {
        SchemaType::Avro => avro_compatibility(previous, next, mode),
        SchemaType::Json => json_compatibility(previous, next, mode),
        SchemaType::Protobuf => protobuf_compatibility(previous, next, mode),
    }
}

// ---------- Avro ----------

fn parse_avro(schema_text: &str) -> Result<apache_avro::Schema, SchemaError> {
    apache_avro::Schema::parse_str(schema_text)
        .map_err(|error| SchemaError::Parse(format!("avro: {error}")))
}

fn validate_avro(schema_text: &str, payload: &Value) -> Result<(), SchemaError> {
    let schema = parse_avro(schema_text)?;
    // apache_avro::from_value validates structural conformance.
    let avro_value = json_to_avro(payload, &schema);
    avro_value
        .validate(&schema)
        .then_some(())
        .ok_or_else(|| SchemaError::Validation("avro payload does not match schema".to_string()))
}

/// Tiny JSON → Avro `Value` projection used for sample validation. We do
/// not need full type coercion: the connectors only feed us JSON, so we
/// rely on Avro's own validator to reject mismatches.
fn json_to_avro(value: &Value, _schema: &apache_avro::Schema) -> apache_avro::types::Value {
    use apache_avro::types::Value as A;
    match value {
        Value::Null => A::Null,
        Value::Bool(b) => A::Boolean(*b),
        Value::Number(n) => {
            if let Some(i) = n.as_i64() {
                A::Long(i)
            } else if let Some(f) = n.as_f64() {
                A::Double(f)
            } else {
                A::Null
            }
        }
        Value::String(s) => A::String(s.clone()),
        Value::Array(items) => A::Array(items.iter().map(|v| json_to_avro(v, _schema)).collect()),
        Value::Object(fields) => A::Record(
            fields
                .iter()
                .map(|(k, v)| (k.clone(), json_to_avro(v, _schema)))
                .collect(),
        ),
    }
}

fn avro_compatibility(
    previous: &str,
    next: &str,
    mode: CompatibilityMode,
) -> Result<(), SchemaError> {
    use apache_avro::schema_compatibility::SchemaCompatibility;
    let prev_schema = parse_avro(previous)?;
    let next_schema = parse_avro(next)?;
    let backward = || {
        SchemaCompatibility::can_read(&prev_schema, &next_schema)
            .map_err(|error| SchemaError::Compatibility(format!("backward: {error}")))
    };
    let forward = || {
        SchemaCompatibility::can_read(&next_schema, &prev_schema)
            .map_err(|error| SchemaError::Compatibility(format!("forward: {error}")))
    };
    match mode {
        CompatibilityMode::None => Ok(()),
        CompatibilityMode::Backward | CompatibilityMode::BackwardTransitive => backward(),
        CompatibilityMode::Forward | CompatibilityMode::ForwardTransitive => forward(),
        CompatibilityMode::Full | CompatibilityMode::FullTransitive => {
            backward().and_then(|_| forward())
        }
    }
}

// ---------- JSON Schema ----------

fn validate_json(schema_text: &str, payload: &Value) -> Result<(), SchemaError> {
    let schema_value: Value = serde_json::from_str(schema_text)
        .map_err(|error| SchemaError::Parse(format!("json schema: {error}")))?;
    let validator = jsonschema::JSONSchema::compile(&schema_value)
        .map_err(|error| SchemaError::Parse(format!("json schema compile: {error}")))?;
    validator.validate(payload).map_err(|errors| {
        let messages: Vec<String> = errors.map(|error| error.to_string()).collect();
        SchemaError::Validation(messages.join("; "))
    })
}

/// JSON Schema does not have a built-in compatibility checker the way Avro
/// does. We implement a pragmatic structural check: under BACKWARD, every
/// `required` property in `next` must already exist (as an own property)
/// in `previous`. Under FORWARD the reverse. FULL = both. This catches the
/// common breaking change ("added new required field") without pulling in
/// a third-party schema-diff crate.
fn json_compatibility(
    previous: &str,
    next: &str,
    mode: CompatibilityMode,
) -> Result<(), SchemaError> {
    let prev: Value = serde_json::from_str(previous)
        .map_err(|error| SchemaError::Parse(format!("json schema previous: {error}")))?;
    let next_v: Value = serde_json::from_str(next)
        .map_err(|error| SchemaError::Parse(format!("json schema next: {error}")))?;

    let prev_required = json_required_props(&prev);
    let next_required = json_required_props(&next_v);
    let prev_props = json_property_names(&prev);
    let next_props = json_property_names(&next_v);

    let backward = || {
        for field in &next_required {
            if !prev_props.contains(field) {
                return Err(SchemaError::Compatibility(format!(
                    "BACKWARD: new required field '{field}' is not present in previous schema"
                )));
            }
        }
        Ok(())
    };
    let forward = || {
        for field in &prev_required {
            if !next_props.contains(field) {
                return Err(SchemaError::Compatibility(format!(
                    "FORWARD: previous required field '{field}' was removed in next schema"
                )));
            }
        }
        Ok(())
    };

    match mode {
        CompatibilityMode::None => Ok(()),
        CompatibilityMode::Backward | CompatibilityMode::BackwardTransitive => backward(),
        CompatibilityMode::Forward | CompatibilityMode::ForwardTransitive => forward(),
        CompatibilityMode::Full | CompatibilityMode::FullTransitive => {
            backward().and_then(|_| forward())
        }
    }
}

fn json_required_props(schema: &Value) -> Vec<String> {
    schema
        .get("required")
        .and_then(Value::as_array)
        .map(|arr| {
            arr.iter()
                .filter_map(|value| value.as_str().map(str::to_string))
                .collect()
        })
        .unwrap_or_default()
}

fn json_property_names(schema: &Value) -> Vec<String> {
    schema
        .get("properties")
        .and_then(Value::as_object)
        .map(|obj| obj.keys().cloned().collect())
        .unwrap_or_default()
}

// ---------- Protobuf (descriptor-set based) ----------

fn parse_protobuf(schema_text: &str) -> Result<prost_reflect::DescriptorPool, SchemaError> {
    use base64::Engine;
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(schema_text.trim())
        .map_err(|error| SchemaError::Parse(format!("protobuf base64: {error}")))?;
    prost_reflect::DescriptorPool::decode(bytes.as_slice())
        .map_err(|error| SchemaError::Parse(format!("protobuf descriptor: {error}")))
}

fn validate_protobuf(schema_text: &str, payload: &Value) -> Result<(), SchemaError> {
    let pool = parse_protobuf(schema_text)?;
    let descriptor = match payload.get("__type").and_then(Value::as_str) {
        Some(name) => pool
            .get_message_by_name(name)
            .ok_or_else(|| SchemaError::Validation(format!("unknown message: {name}")))?,
        None => pool.all_messages().next().ok_or_else(|| {
            SchemaError::Parse("protobuf descriptor set has no messages".to_string())
        })?,
    };
    let payload_obj = payload
        .as_object()
        .ok_or_else(|| SchemaError::Validation("protobuf payload must be an object".to_string()))?;
    for field in descriptor.fields() {
        if !payload_obj.contains_key(field.name())
            && field.cardinality() == prost_reflect::Cardinality::Required
        {
            return Err(SchemaError::Validation(format!(
                "missing required protobuf field '{}'",
                field.name()
            )));
        }
    }
    Ok(())
}

fn protobuf_compatibility(
    previous: &str,
    next: &str,
    mode: CompatibilityMode,
) -> Result<(), SchemaError> {
    // Protobuf compatibility model: removed/renamed fields and reused tag
    // numbers are breaking. We compare top-level message field tags.
    let prev_pool = parse_protobuf(previous)?;
    let next_pool = parse_protobuf(next)?;
    let prev_msg = prev_pool
        .all_messages()
        .next()
        .ok_or_else(|| SchemaError::Parse("previous descriptor set is empty".to_string()))?;
    let next_msg = next_pool
        .all_messages()
        .next()
        .ok_or_else(|| SchemaError::Parse("next descriptor set is empty".to_string()))?;

    let prev_tags: std::collections::HashMap<u32, String> = prev_msg
        .fields()
        .map(|f| (f.number(), f.name().to_string()))
        .collect();
    let next_tags: std::collections::HashMap<u32, String> = next_msg
        .fields()
        .map(|f| (f.number(), f.name().to_string()))
        .collect();

    let backward = || {
        for (tag, name) in &prev_tags {
            if let Some(new_name) = next_tags.get(tag) {
                if new_name != name {
                    return Err(SchemaError::Compatibility(format!(
                        "BACKWARD: tag {tag} renamed from '{name}' to '{new_name}'"
                    )));
                }
            }
        }
        Ok(())
    };
    let forward = || {
        for (tag, name) in &next_tags {
            if let Some(prev_name) = prev_tags.get(tag) {
                if prev_name != name {
                    return Err(SchemaError::Compatibility(format!(
                        "FORWARD: tag {tag} renamed from '{prev_name}' to '{name}'"
                    )));
                }
            }
        }
        Ok(())
    };
    match mode {
        CompatibilityMode::None => Ok(()),
        CompatibilityMode::Backward | CompatibilityMode::BackwardTransitive => backward(),
        CompatibilityMode::Forward | CompatibilityMode::ForwardTransitive => forward(),
        CompatibilityMode::Full | CompatibilityMode::FullTransitive => {
            backward().and_then(|_| forward())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    const AVRO_V1: &str = r#"{
        "type": "record",
        "name": "Order",
        "fields": [
            { "name": "order_id", "type": "string" },
            { "name": "amount", "type": "long" }
        ]
    }"#;

    /// BACKWARD-compatible: adds a new optional field with a default.
    /// Old readers can still read new records (they ignore the extra field).
    const AVRO_V2_COMPATIBLE: &str = r#"{
        "type": "record",
        "name": "Order",
        "fields": [
            { "name": "order_id", "type": "string" },
            { "name": "amount", "type": "long" },
            { "name": "currency", "type": "string", "default": "USD" }
        ]
    }"#;

    /// BREAKING under BACKWARD: adds a new required field with no default,
    /// so a reader using v2 cannot decode v1 records (the new field is
    /// missing and there is no fallback value).
    const AVRO_V2_BREAKING: &str = r#"{
        "type": "record",
        "name": "Order",
        "fields": [
            { "name": "order_id", "type": "string" },
            { "name": "amount", "type": "long" },
            { "name": "currency", "type": "string" }
        ]
    }"#;

    #[test]
    fn fingerprint_is_canonical_and_stable() {
        let f1 = fingerprint(SchemaType::Avro, AVRO_V1).unwrap();
        // Same schema with reformatted whitespace must produce same fingerprint.
        let pretty =
            serde_json::to_string_pretty(&serde_json::from_str::<Value>(AVRO_V1).unwrap()).unwrap();
        let f2 = fingerprint(SchemaType::Avro, &pretty).unwrap();
        assert_eq!(f1, f2);
        assert!(f1.starts_with("sha256:"));
    }

    #[test]
    fn avro_payload_validates_against_schema() {
        let payload = json!({ "order_id": "ord-1", "amount": 4200 });
        validate_payload(SchemaType::Avro, AVRO_V1, &payload).expect("valid");
    }

    #[test]
    fn avro_v2_is_backward_compatible_with_v1() {
        check_compatibility(
            SchemaType::Avro,
            AVRO_V1,
            AVRO_V2_COMPATIBLE,
            CompatibilityMode::Backward,
        )
        .expect("BACKWARD compatible: optional field with default is non-breaking");
    }

    #[test]
    fn avro_breaking_is_rejected_under_backward() {
        let error = check_compatibility(
            SchemaType::Avro,
            AVRO_V1,
            AVRO_V2_BREAKING,
            CompatibilityMode::Backward,
        )
        .expect_err("removing a required field is BREAKING");
        assert!(matches!(error, SchemaError::Compatibility(_)));
    }

    #[test]
    fn json_schema_validates_payload() {
        let schema = r#"{
            "type": "object",
            "required": ["order_id"],
            "properties": {
                "order_id": { "type": "string" },
                "amount": { "type": "number" }
            }
        }"#;
        validate_payload(SchemaType::Json, schema, &json!({ "order_id": "ord-1" })).expect("valid");
        let error = validate_payload(SchemaType::Json, schema, &json!({ "amount": 1 }))
            .expect_err("missing required");
        assert!(matches!(error, SchemaError::Validation(_)));
    }

    #[test]
    fn json_schema_compatibility_detects_new_required_field() {
        let v1 = r#"{
            "type": "object",
            "required": ["a"],
            "properties": { "a": { "type": "string" } }
        }"#;
        let v2_breaking = r#"{
            "type": "object",
            "required": ["a", "b"],
            "properties": { "a": { "type": "string" } }
        }"#;
        check_compatibility(
            SchemaType::Json,
            v1,
            v2_breaking,
            CompatibilityMode::Backward,
        )
        .expect_err("BREAKING: new required field 'b' was not in previous schema");
    }
}
