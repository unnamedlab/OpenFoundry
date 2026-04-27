use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::models::pipeline::{PipelineNode, PipelineRetryPolicy, PipelineScheduleConfig};

fn default_status() -> String {
    "draft".to_string()
}

fn default_distributed_worker_count() -> usize {
    1
}

#[derive(Debug, Clone, Deserialize)]
#[serde(default)]
pub struct ValidatePipelineRequest {
    #[serde(default = "default_status")]
    pub status: String,
    pub nodes: Vec<PipelineNode>,
    pub schedule_config: PipelineScheduleConfig,
    pub retry_policy: PipelineRetryPolicy,
}

impl Default for ValidatePipelineRequest {
    fn default() -> Self {
        Self {
            status: default_status(),
            nodes: Vec::new(),
            schedule_config: PipelineScheduleConfig::default(),
            retry_policy: PipelineRetryPolicy::default(),
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(default)]
pub struct CompilePipelineRequest {
    #[serde(flatten)]
    pub pipeline: ValidatePipelineRequest,
    pub start_from_node: Option<String>,
    #[serde(default = "default_distributed_worker_count")]
    pub distributed_worker_count: usize,
}

impl Default for CompilePipelineRequest {
    fn default() -> Self {
        Self {
            pipeline: ValidatePipelineRequest::default(),
            start_from_node: None,
            distributed_worker_count: default_distributed_worker_count(),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct PipelineGraphSummary {
    pub node_count: usize,
    pub edge_count: usize,
    pub root_nodes: Vec<String>,
    pub leaf_nodes: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct PipelineValidationResponse {
    pub valid: bool,
    pub errors: Vec<String>,
    pub warnings: Vec<String>,
    pub next_run_at: Option<DateTime<Utc>>,
    pub summary: PipelineGraphSummary,
}

#[derive(Debug, Clone, Serialize)]
pub struct ExecutablePlan {
    pub start_from_node: Option<String>,
    pub node_order: Vec<String>,
    pub execution_stages: Vec<Vec<String>>,
    pub reachable_node_ids: Vec<String>,
    pub pruned_node_ids: Vec<String>,
    pub distributed_worker_count: usize,
    pub retry_policy: PipelineRetryPolicy,
    pub mode: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct CompilePipelineResponse {
    pub validation: PipelineValidationResponse,
    pub plan: ExecutablePlan,
}

#[derive(Debug, Clone, Serialize)]
pub struct PrunePipelineResponse {
    pub validation: PipelineValidationResponse,
    pub start_from_node: Option<String>,
    pub reachable_node_ids: Vec<String>,
    pub pruned_node_ids: Vec<String>,
}