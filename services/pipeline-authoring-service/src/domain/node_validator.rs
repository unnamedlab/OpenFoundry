//! Thin adapter around [`pipeline_expression::node_check`].
//!
//! The actual validation lives in the lib so it stays pure-Rust and
//! reusable. This adapter only translates between the persisted
//! `PipelineNode` model (Rust struct with serde) and the JSON shape the
//! lib accepts.

use pipeline_expression::node_check;

use crate::models::pipeline::PipelineNode;

pub use pipeline_expression::{
    NodeValidationError, NodeValidationReport, PipelineValidationReport,
};

pub fn validate_pipeline_nodes(
    pipeline_id: &str,
    nodes: &[PipelineNode],
) -> PipelineValidationReport {
    let json = serde_json::to_value(nodes).unwrap_or_else(|_| serde_json::Value::Array(vec![]));
    node_check::validate_nodes_json(pipeline_id, &json)
}
