//! TASK M — Unit coverage for the documented Foundry scale & property
//! limits enforced by `ontology_kernel::handlers::actions`. These tests
//! intentionally avoid Postgres so they run on every commit.

use ontology_kernel::handlers::actions::{
    estimate_edit_bytes, scale_limits, validate_parameter_list_sizes,
};
use ontology_kernel::models::action_type::ActionInputField;
use serde_json::{Value, json};

fn field(name: &str, property_type: &str) -> ActionInputField {
    ActionInputField {
        name: name.to_string(),
        display_name: None,
        description: None,
        property_type: property_type.to_string(),
        required: false,
        default_value: None,
        struct_fields: None,
    }
}

#[test]
fn primitive_list_within_cap_passes() {
    let schema = vec![field("tags", "array")];
    let parameters = json!({ "tags": vec![1; scale_limits::MAX_LIST_PRIMITIVE] });
    assert!(validate_parameter_list_sizes(&schema, &parameters).is_ok());
}

#[test]
fn primitive_list_over_cap_is_rejected() {
    let schema = vec![field("tags", "array")];
    let parameters = json!({ "tags": vec![1; scale_limits::MAX_LIST_PRIMITIVE + 1] });
    let error = validate_parameter_list_sizes(&schema, &parameters).unwrap_err();
    assert!(error.contains("tags"));
    assert!(error.contains(&scale_limits::MAX_LIST_PRIMITIVE.to_string()));
}

#[test]
fn vector_parameter_uses_primitive_cap() {
    let schema = vec![field("embedding", "vector")];
    let parameters = json!({ "embedding": vec![0.0; scale_limits::MAX_LIST_PRIMITIVE + 1] });
    assert!(validate_parameter_list_sizes(&schema, &parameters).is_err());
}

#[test]
fn object_reference_list_within_cap_passes() {
    let schema = vec![field("targets", "object_reference_list")];
    let parameters = json!({
        "targets": (0..scale_limits::MAX_OBJECT_REFERENCE_LIST)
            .map(|index| index.to_string())
            .collect::<Vec<_>>()
    });
    assert!(validate_parameter_list_sizes(&schema, &parameters).is_ok());
}

#[test]
fn object_reference_list_over_cap_is_rejected() {
    let schema = vec![field("targets", "object_reference_list")];
    let parameters = json!({
        "targets": (0..(scale_limits::MAX_OBJECT_REFERENCE_LIST + 1))
            .map(|index| index.to_string())
            .collect::<Vec<_>>()
    });
    let error = validate_parameter_list_sizes(&schema, &parameters).unwrap_err();
    assert!(error.contains("targets"));
    assert!(error.contains(&scale_limits::MAX_OBJECT_REFERENCE_LIST.to_string()));
}

#[test]
fn missing_parameter_skips_validation() {
    let schema = vec![field("targets", "array")];
    let parameters = json!({});
    assert!(validate_parameter_list_sizes(&schema, &parameters).is_ok());
}

#[test]
fn non_list_parameter_is_ignored() {
    let schema = vec![field("title", "string")];
    let parameters = json!({ "title": "hello" });
    assert!(validate_parameter_list_sizes(&schema, &parameters).is_ok());
}

#[test]
fn estimate_edit_bytes_matches_serialized_size() {
    let value: Value = json!({ "name": "object", "items": [1, 2, 3] });
    let serialized = serde_json::to_vec(&value).unwrap();
    assert_eq!(estimate_edit_bytes(&value), serialized.len());
}

#[test]
fn three_megabyte_payload_estimate_exceeds_cap() {
    let payload = "x".repeat(scale_limits::MAX_EDIT_BYTES + 1);
    let value = Value::String(payload);
    assert!(estimate_edit_bytes(&value) > scale_limits::MAX_EDIT_BYTES);
}

#[test]
fn scale_limit_constants_match_documented_values() {
    // These constants are part of the contract that backs the
    // `failure_type=scale_limit` HTTP responses; bumping any of them is a
    // user-facing behaviour change and should fail this test.
    assert_eq!(scale_limits::MAX_OBJECT_TYPES_PER_SUBMISSION, 50);
    assert_eq!(scale_limits::MAX_OBJECTS_PER_SUBMISSION, 10_000);
    assert_eq!(scale_limits::MAX_EDIT_BYTES, 3 * 1024 * 1024);
    assert_eq!(scale_limits::MAX_LIST_PRIMITIVE, 10_000);
    assert_eq!(scale_limits::MAX_OBJECT_REFERENCE_LIST, 1_000);
    assert_eq!(scale_limits::MAX_NOTIFICATION_RECIPIENTS, 500);
    assert_eq!(scale_limits::MAX_NOTIFICATION_RECIPIENTS_FROM_FUNCTION, 50);
}
