//! FASE 4 — preview engine smoke + the `cast → filter → join` chain
//! mandated by the migration plan.
//!
//! Drives `pipeline_expression::preview::preview_node` (the engine the
//! service handler dispatches to) with a static fake seed loader so
//! the test stays pure-Rust (no DB, no DataFusion compute). Asserts:
//!
//!   * column projection is stable.
//!   * `cast` materialises the target type per row.
//!   * `filter` keeps only rows whose predicate holds.
//!   * `join` performs an inner equijoin on the requested keys.

use std::collections::HashMap;

use pipeline_expression::eval::Row;
use pipeline_expression::preview::{
    JsonPipelineNode, PreviewNodeView, SeedLoader, preview_node,
};
use serde_json::{Value, json};

struct StaticLoader {
    rows: HashMap<String, Vec<Row>>,
}

impl SeedLoader for StaticLoader {
    fn load<N: PreviewNodeView>(&self, node: &N, _sample_size: usize) -> Result<Vec<Row>, String> {
        Ok(self.rows.get(node.id()).cloned().unwrap_or_default())
    }
}

fn row(pairs: &[(&str, Value)]) -> Row {
    pairs
        .iter()
        .map(|(k, v)| ((*k).to_string(), v.clone()))
        .collect()
}

#[test]
fn preview_chain_cast_filter_join_returns_only_matching_high_value_orders() {
    let nodes = vec![
        JsonPipelineNode::new("orders", "passthrough", json!({}), &[]),
        JsonPipelineNode::new("customers", "passthrough", json!({}), &[]),
        JsonPipelineNode::new(
            "orders_typed",
            "cast",
            json!({ "columns": ["amount"], "cast_target": "DOUBLE" }),
            &["orders"],
        ),
        JsonPipelineNode::new(
            "joined",
            "join",
            json!({ "how": "inner", "on": ["customer_id"] }),
            &["orders_typed", "customers"],
        ),
        JsonPipelineNode::new(
            "high_value",
            "filter",
            json!({ "predicate": "amount > 100" }),
            &["joined"],
        ),
    ];

    let loader = StaticLoader {
        rows: HashMap::from([
            (
                "orders".to_string(),
                vec![
                    row(&[
                        ("order_id", json!("o1")),
                        ("customer_id", json!("c1")),
                        ("amount", json!("250")),
                    ]),
                    row(&[
                        ("order_id", json!("o2")),
                        ("customer_id", json!("c2")),
                        ("amount", json!("80")),
                    ]),
                    row(&[
                        ("order_id", json!("o3")),
                        ("customer_id", json!("c1")),
                        ("amount", json!("125")),
                    ]),
                ],
            ),
            (
                "customers".to_string(),
                vec![
                    row(&[
                        ("customer_id", json!("c1")),
                        ("name", json!("Ada Lovelace")),
                    ]),
                    row(&[
                        ("customer_id", json!("c2")),
                        ("name", json!("Alan Turing")),
                    ]),
                ],
            ),
        ]),
    };

    let preview = preview_node("pipeline-1", "high_value", &nodes, &loader, Some(50)).unwrap();

    assert_eq!(preview.node_id, "high_value");
    assert!(preview.columns.contains(&"order_id".to_string()));
    assert!(preview.columns.contains(&"name".to_string()));

    // Two rows survive: o1 (250) and o3 (125). o2 (80) is filtered out.
    assert_eq!(preview.rows.len(), 2);
    let order_ids: Vec<String> = preview
        .rows
        .iter()
        .filter_map(|r| r.get("order_id").and_then(Value::as_str).map(String::from))
        .collect();
    assert!(order_ids.contains(&"o1".to_string()));
    assert!(order_ids.contains(&"o3".to_string()));
    assert!(!order_ids.contains(&"o2".to_string()));

    // Cast must have promoted "amount" to numeric so the filter could
    // compare against 100.
    for r in &preview.rows {
        let amount = r.get("amount").expect("amount column present");
        assert!(amount.is_number(), "expected amount to be numeric, got {amount:?}");
    }

    // The chain must include every upstream node we relied on.
    for required in ["orders", "customers", "orders_typed", "joined", "high_value"] {
        assert!(
            preview.source_chain.iter().any(|n| n == required),
            "chain missing '{required}': {:?}",
            preview.source_chain
        );
    }

    assert!(preview.fresh);
    assert_eq!(preview.sample_size, 2);
}

#[test]
fn preview_unknown_node_returns_node_not_found() {
    let nodes = vec![JsonPipelineNode::new("a", "passthrough", json!({}), &[])];
    let loader = StaticLoader { rows: HashMap::new() };
    let err =
        preview_node("pipeline-2", "missing", &nodes, &loader, None).expect_err("expected error");
    assert!(err.to_string().contains("not found"), "{err}");
}

#[test]
fn preview_filter_with_unparseable_predicate_returns_transform_error() {
    let nodes = vec![
        JsonPipelineNode::new("a", "passthrough", json!({}), &[]),
        JsonPipelineNode::new(
            "f",
            "filter",
            json!({ "predicate": "this is broken" }),
            &["a"],
        ),
    ];
    let loader = StaticLoader {
        rows: HashMap::from([(
            "a".to_string(),
            vec![row(&[("x", json!(1))])],
        )]),
    };
    let err = preview_node("pipeline-3", "f", &nodes, &loader, None).expect_err("expected error");
    assert!(
        err.to_string().contains("filter") || err.to_string().contains("parse"),
        "{err}"
    );
}
