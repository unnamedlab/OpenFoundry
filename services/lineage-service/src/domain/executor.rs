use serde_json::Value;
use uuid::Uuid;

use crate::{AppState, models::pipeline::Pipeline, models::run::PipelineRun};

/// `lineage-service` owns lineage reads and workflow fan-out; it does not
/// embed the Pipeline Builder execution engine. Pipeline build candidates
/// therefore surface as skipped until this service grows a dedicated
/// execution handoff.
pub async fn start_pipeline_run(
    _state: &AppState,
    pipeline: &Pipeline,
    _started_by: Option<Uuid>,
    _trigger_type: &str,
    _from_node_id: Option<String>,
    _retry_of_run_id: Option<Uuid>,
    _attempt_number: i32,
    _distributed_worker_count: usize,
    _skip_unchanged: bool,
    _context: Value,
) -> Result<PipelineRun, String> {
    Err(format!(
        "pipeline lineage build handoff is not wired in lineage-service for pipeline {}",
        pipeline.id
    ))
}
