//! Per-node validation entry point used by `pipeline-authoring-service`
//! to power `POST /api/v1/pipelines/{id}/validate`.
//!
//! The function accepts the pipeline DAG as a `serde_json::Value` (an
//! array of node objects shaped like the persisted JSON) so the lib
//! does not need to know the service's full `PipelineNode` Rust type.
//! It returns a [`PipelineValidationReport`] with one entry per node.

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::catalog::transform_signature;
use crate::infer::{ColumnEnv, TypeError, infer_expr};
use crate::parser::parse_expr;
use crate::types::PipelineType;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct NodeValidationError {
    pub node_id: String,
    pub column: Option<String>,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct NodeValidationReport {
    pub node_id: String,
    pub status: String,
    pub errors: Vec<NodeValidationError>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct PipelineValidationReport {
    pub pipeline_id: String,
    pub all_valid: bool,
    pub nodes: Vec<NodeValidationReport>,
}

const STATUS_VALID: &str = "VALID";
const STATUS_INVALID: &str = "INVALID";

/// Validate every node in `nodes_json` (expected to be a JSON array of
/// node objects with `id`, `transform_type`, `config`, `depends_on`
/// fields). Returns one report per node — the order matches the input.
pub fn validate_nodes_json(pipeline_id: &str, nodes_json: &Value) -> PipelineValidationReport {
    let empty: Vec<Value> = Vec::new();
    let nodes = nodes_json.as_array().unwrap_or(&empty);

    let mut node_reports = Vec::with_capacity(nodes.len());
    for node in nodes {
        node_reports.push(validate_one(node, nodes));
    }
    let all_valid = node_reports.iter().all(|r| r.status == STATUS_VALID);
    PipelineValidationReport {
        pipeline_id: pipeline_id.to_string(),
        all_valid,
        nodes: node_reports,
    }
}

fn validate_one(node: &Value, all_nodes: &[Value]) -> NodeValidationReport {
    let id = node_id(node).unwrap_or_default();
    let transform = node
        .get("transform_type")
        .and_then(Value::as_str)
        .unwrap_or("");
    let config = node.get("config").cloned().unwrap_or(Value::Null);
    let depends_on: Vec<String> = node
        .get("depends_on")
        .and_then(Value::as_array)
        .map(|a| a.iter().filter_map(|v| v.as_str().map(String::from)).collect())
        .unwrap_or_default();

    let env = synth_env(&depends_on, all_nodes);
    let mut errors = Vec::new();

    match transform.to_ascii_lowercase().as_str() {
        "passthrough" => {}
        "filter" => check_filter(&id, &config, &env, &mut errors),
        "cast" | "title_case" | "clean_string" => {
            check_columns_array(&id, transform, &config, &mut errors);
        }
        "join" => {
            check_required_string(&id, "how", &config, &mut errors);
            check_required_array(&id, "on", &config, &mut errors);
        }
        "union" => {
            if depends_on.len() < 2 {
                errors.push(NodeValidationError {
                    node_id: id.clone(),
                    column: None,
                    message: "union requires at least 2 upstream nodes".into(),
                });
            }
        }
        "group_by" => {
            check_required_string_array(&id, "keys", &config, &mut errors);
            check_required_array(&id, "aggregations", &config, &mut errors);
        }
        "window" => {
            check_required_string_array(&id, "partition_by", &config, &mut errors);
            check_required_string_array(&id, "order_by", &config, &mut errors);
        }
        "pivot" => {
            check_required_string(&id, "pivot_column", &config, &mut errors);
            check_required_string(&id, "value_column", &config, &mut errors);
        }
        other => {
            // Code transforms (sql/python/llm/wasm) and media transforms
            // are checked elsewhere — only verify the catalog has heard
            // of the name.
            if transform_signature(other).is_none() && !is_known_other(other) {
                // Unknown transform type — surface a single error.
                errors.push(NodeValidationError {
                    node_id: id.clone(),
                    column: None,
                    message: format!("unknown transform_type '{transform}'"),
                });
            }
        }
    }

    let status = if errors.is_empty() {
        STATUS_VALID
    } else {
        STATUS_INVALID
    };
    NodeValidationReport {
        node_id: id,
        status: status.to_string(),
        errors,
    }
}

fn is_known_other(name: &str) -> bool {
    matches!(
        name,
        "sql"
            | "python"
            | "llm"
            | "wasm"
            | "media_set_input"
            | "media_set_output"
            | "media_transform"
            | "convert_media_set_to_table_rows"
            | "get_media_references"
            | "SYNC"
            | "HEALTH_CHECK"
            | "ANALYTICAL"
            | "EXPORT"
    )
}

fn check_filter(
    node_id: &str,
    config: &Value,
    env: &ColumnEnv,
    errors: &mut Vec<NodeValidationError>,
) {
    let predicate = match config.get("predicate").and_then(Value::as_str) {
        Some(s) if !s.trim().is_empty() => s.to_string(),
        _ => {
            errors.push(NodeValidationError {
                node_id: node_id.to_string(),
                column: None,
                message: "filter requires a non-empty `predicate` string".into(),
            });
            return;
        }
    };

    let parsed = match parse_expr(&predicate) {
        Ok(p) => p,
        Err(e) => {
            errors.push(NodeValidationError {
                node_id: node_id.to_string(),
                column: None,
                message: format!("predicate parse error: {e}"),
            });
            return;
        }
    };

    // Empty env => the upstream nodes don't expose schema info yet;
    // suppress UnknownColumn errors so the squiggle UI doesn't spam.
    let permissive = env.is_empty();

    match infer_expr(&parsed, env) {
        Ok(t) => {
            if !matches!(t, PipelineType::Boolean) {
                errors.push(NodeValidationError {
                    node_id: node_id.to_string(),
                    column: None,
                    message: format!("predicate must return Boolean, got {t:?}"),
                });
            }
        }
        Err(type_errors) => {
            for type_error in type_errors {
                if permissive && matches!(type_error, TypeError::UnknownColumn(_)) {
                    continue;
                }
                errors.push(NodeValidationError {
                    node_id: node_id.to_string(),
                    column: column_from_type_error(&type_error),
                    message: type_error.to_string(),
                });
            }
        }
    }
}

fn column_from_type_error(error: &TypeError) -> Option<String> {
    match error {
        TypeError::UnknownColumn(name) => Some(name.clone()),
        _ => None,
    }
}

fn check_columns_array(
    node_id: &str,
    transform: &str,
    config: &Value,
    errors: &mut Vec<NodeValidationError>,
) {
    match config.get("columns") {
        Some(Value::Array(arr)) if arr.iter().all(Value::is_string) && !arr.is_empty() => {}
        Some(_) => errors.push(NodeValidationError {
            node_id: node_id.to_string(),
            column: None,
            message: format!(
                "{transform} requires `columns` to be a non-empty array of strings"
            ),
        }),
        None => errors.push(NodeValidationError {
            node_id: node_id.to_string(),
            column: None,
            message: format!("{transform} requires a `columns` config key"),
        }),
    }
}

fn check_required_string_array(
    node_id: &str,
    key: &str,
    config: &Value,
    errors: &mut Vec<NodeValidationError>,
) {
    match config.get(key) {
        Some(Value::Array(arr)) if arr.iter().all(Value::is_string) => {}
        _ => errors.push(NodeValidationError {
            node_id: node_id.to_string(),
            column: None,
            message: format!("`{key}` must be an array of strings"),
        }),
    }
}

fn check_required_array(
    node_id: &str,
    key: &str,
    config: &Value,
    errors: &mut Vec<NodeValidationError>,
) {
    match config.get(key) {
        Some(Value::Array(_)) => {}
        _ => errors.push(NodeValidationError {
            node_id: node_id.to_string(),
            column: None,
            message: format!("`{key}` must be an array"),
        }),
    }
}

fn check_required_string(
    node_id: &str,
    key: &str,
    config: &Value,
    errors: &mut Vec<NodeValidationError>,
) {
    match config.get(key) {
        Some(Value::String(s)) if !s.trim().is_empty() => {}
        _ => errors.push(NodeValidationError {
            node_id: node_id.to_string(),
            column: None,
            message: format!("`{key}` must be a non-empty string"),
        }),
    }
}

fn synth_env(depends_on: &[String], all_nodes: &[Value]) -> ColumnEnv {
    let mut env = ColumnEnv::new();
    for upstream_id in depends_on {
        let Some(upstream) = all_nodes.iter().find(|n| node_id(n).as_deref() == Some(upstream_id))
        else {
            continue;
        };
        let cfg = upstream.get("config").cloned().unwrap_or(Value::Null);
        if let Some(arr) = cfg.get("columns").and_then(Value::as_array) {
            for col in arr {
                if let Some(name) = col.as_str() {
                    env.insert(name, PipelineType::String);
                }
            }
        }
        if let Some(arr) = cfg.get("output_columns").and_then(Value::as_array) {
            for col in arr {
                if let Some(name) = col.as_str() {
                    env.insert(name, PipelineType::String);
                }
            }
        }
    }
    env
}

fn node_id(node: &Value) -> Option<String> {
    node.get("id")
        .and_then(Value::as_str)
        .map(str::to_string)
}
