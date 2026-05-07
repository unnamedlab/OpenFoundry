//! FASE 3 — exercises the per-node type-safe validator that powers
//! `POST /api/v1/pipelines/{id}/validate`.
//!
//! Pure-logic: builds the same JSON the handler hands to
//! `pipeline_expression::node_check::validate_nodes_json` and asserts on
//! the report shape. The HTTP wrapper is a 6-line shim around this
//! function (read pipeline → call lib → serialise) so this test covers
//! the substantive contract.

use pipeline_expression::node_check::{NodeValidationError, validate_nodes_json};
use serde_json::json;

#[test]
fn passthrough_only_pipeline_is_valid() {
    let nodes = json!([
        { "id": "src", "transform_type": "passthrough", "config": {}, "depends_on": [] }
    ]);
    let report = validate_nodes_json("pipeline-1", &nodes);
    assert!(report.all_valid);
    assert_eq!(report.nodes.len(), 1);
    assert_eq!(report.nodes[0].status, "VALID");
}

#[test]
fn filter_with_boolean_predicate_against_upstream_columns_is_valid() {
    let nodes = json!([
        {
            "id": "src",
            "transform_type": "passthrough",
            "config": { "columns": ["age", "name"] },
            "depends_on": []
        },
        {
            "id": "f",
            "transform_type": "filter",
            "config": { "predicate": "age = age" },
            "depends_on": ["src"]
        }
    ]);
    let report = validate_nodes_json("pipeline-2", &nodes);
    assert!(report.all_valid, "{:?}", report);
    let f = &report.nodes.iter().find(|r| r.node_id == "f").unwrap();
    assert_eq!(f.status, "VALID");
}

#[test]
fn filter_with_unparseable_predicate_is_invalid() {
    let nodes = json!([
        {
            "id": "f",
            "transform_type": "filter",
            "config": { "predicate": "this is (not parsable" },
            "depends_on": []
        }
    ]);
    let report = validate_nodes_json("pipeline-3", &nodes);
    assert!(!report.all_valid);
    let f = &report.nodes[0];
    assert_eq!(f.status, "INVALID");
    assert!(f.errors.iter().any(|e: &NodeValidationError| e.message.contains("parse error")));
}

#[test]
fn filter_predicate_returning_non_boolean_is_invalid() {
    // Predicate `42` types as Integer; upstream env is irrelevant. The
    // filter rule rejects any non-Boolean shape with a "must return
    // Boolean" diagnostic.
    let nodes = json!([
        {
            "id": "f",
            "transform_type": "filter",
            "config": { "predicate": "42" },
            "depends_on": []
        }
    ]);
    let report = validate_nodes_json("pipeline-4", &nodes);
    let f = report.nodes.iter().find(|r| r.node_id == "f").unwrap();
    assert_eq!(f.status, "INVALID");
    assert!(
        f.errors.iter().any(|e| e.message.contains("Boolean")),
        "expected Boolean error, got {:?}",
        f.errors
    );
}

#[test]
fn filter_with_unknown_column_surfaces_column_in_error() {
    let nodes = json!([
        {
            "id": "src",
            "transform_type": "passthrough",
            "config": { "columns": ["age"] },
            "depends_on": []
        },
        {
            "id": "f",
            "transform_type": "filter",
            "config": { "predicate": "missing > 0" },
            "depends_on": ["src"]
        }
    ]);
    let report = validate_nodes_json("pipeline-5", &nodes);
    let f = report.nodes.iter().find(|r| r.node_id == "f").unwrap();
    assert_eq!(f.status, "INVALID");
    let unknown = f
        .errors
        .iter()
        .find(|e| e.message.contains("missing"))
        .expect("unknown column error not surfaced");
    assert_eq!(unknown.column.as_deref(), Some("missing"));
}

#[test]
fn filter_without_upstream_schema_does_not_complain_about_columns() {
    // No upstream "columns" → permissive: unknown-column errors are
    // suppressed, but the predicate still has to type-check structurally.
    let nodes = json!([
        {
            "id": "f",
            "transform_type": "filter",
            "config": { "predicate": "is_null(some_col)" },
            "depends_on": []
        }
    ]);
    let report = validate_nodes_json("pipeline-6", &nodes);
    assert!(report.all_valid, "{:?}", report);
}

#[test]
fn cast_node_requires_columns_array() {
    let nodes = json!([
        { "id": "c", "transform_type": "cast", "config": {}, "depends_on": [] }
    ]);
    let report = validate_nodes_json("pipeline-7", &nodes);
    let c = &report.nodes[0];
    assert_eq!(c.status, "INVALID");
    assert!(c.errors.iter().any(|e| e.message.contains("columns")));
}

#[test]
fn cast_node_with_columns_is_valid() {
    let nodes = json!([
        {
            "id": "c",
            "transform_type": "cast",
            "config": { "columns": ["age"] },
            "depends_on": []
        }
    ]);
    let report = validate_nodes_json("pipeline-8", &nodes);
    assert!(report.all_valid);
}

#[test]
fn join_requires_how_and_on() {
    let nodes = json!([
        { "id": "a", "transform_type": "passthrough", "config": {}, "depends_on": [] },
        { "id": "b", "transform_type": "passthrough", "config": {}, "depends_on": [] },
        { "id": "j", "transform_type": "join", "config": {}, "depends_on": ["a", "b"] }
    ]);
    let report = validate_nodes_json("pipeline-9", &nodes);
    let j = report.nodes.iter().find(|r| r.node_id == "j").unwrap();
    assert_eq!(j.status, "INVALID");
    assert!(j.errors.iter().any(|e| e.message.contains("how")));
    assert!(j.errors.iter().any(|e| e.message.contains("on")));
}

#[test]
fn union_requires_two_upstreams() {
    let nodes = json!([
        { "id": "a", "transform_type": "passthrough", "config": {}, "depends_on": [] },
        { "id": "u", "transform_type": "union", "config": {}, "depends_on": ["a"] }
    ]);
    let report = validate_nodes_json("pipeline-10", &nodes);
    let u = report.nodes.iter().find(|r| r.node_id == "u").unwrap();
    assert_eq!(u.status, "INVALID");
    assert!(u.errors.iter().any(|e| e.message.contains("at least 2")));
}

#[test]
fn group_by_requires_keys_and_aggregations() {
    let nodes = json!([
        { "id": "g", "transform_type": "group_by", "config": {}, "depends_on": [] }
    ]);
    let report = validate_nodes_json("pipeline-11", &nodes);
    let g = &report.nodes[0];
    assert_eq!(g.status, "INVALID");
    assert!(g.errors.iter().any(|e| e.message.contains("keys")));
    assert!(g.errors.iter().any(|e| e.message.contains("aggregations")));
}

#[test]
fn unknown_transform_type_is_invalid() {
    let nodes = json!([
        { "id": "x", "transform_type": "explode", "config": {}, "depends_on": [] }
    ]);
    let report = validate_nodes_json("pipeline-12", &nodes);
    let x = &report.nodes[0];
    assert_eq!(x.status, "INVALID");
    assert!(x.errors.iter().any(|e| e.message.contains("explode")));
}

#[test]
fn report_pipeline_id_round_trips() {
    let nodes = json!([]);
    let report = validate_nodes_json("ri.foundry.main.pipeline.abc", &nodes);
    assert_eq!(report.pipeline_id, "ri.foundry.main.pipeline.abc");
    assert!(report.all_valid);
    assert!(report.nodes.is_empty());
}
