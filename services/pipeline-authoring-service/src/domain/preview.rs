//! Thin adapter around [`pipeline_expression::preview`].
//!
//! Converts the persisted [`PipelineNode`] model into the lib's JSON
//! shape and dispatches to the engine. Keeps the wire / DB plumbing in
//! the service while the actual chain resolver + transforms live in
//! the lib so they stay pure-Rust + testable.

use pipeline_expression::preview::{
    self, DeterministicSeedLoader, JsonPipelineNode, PreviewError, PreviewOutput,
};

use crate::models::pipeline::PipelineNode;

pub fn preview_pipeline_node(
    pipeline_id: &str,
    node_id: &str,
    nodes: &[PipelineNode],
    sample_size: Option<usize>,
) -> Result<PreviewOutput, PreviewError> {
    let lib_nodes: Vec<JsonPipelineNode> = nodes
        .iter()
        .map(|n| JsonPipelineNode {
            id: n.id.clone(),
            transform_type: n.transform_type.clone(),
            config: n.config.clone(),
            depends_on: n.depends_on.clone(),
        })
        .collect();
    let loader = DeterministicSeedLoader {
        pipeline_id: pipeline_id.to_string(),
    };
    preview::preview_node(pipeline_id, node_id, &lib_nodes, &loader, sample_size)
}
