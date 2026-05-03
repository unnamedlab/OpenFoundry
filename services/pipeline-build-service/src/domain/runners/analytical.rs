//! `ANALYTICAL` job runner — Foundry "Analytical applications support
//! defining logic that transforms datasets" (Datasets and Object Sets
//! doc).
//!
//! Materialises an object-set query into the job's single output
//! dataset. The query language is opaque to the runner — it is
//! captured verbatim in `JobSpec.logic_payload` and executed by the
//! engine when this runner integrates with the query plane.
//!
//! The current implementation captures every parameter and emits a
//! deterministic `output_content_hash` derived from the query body
//! so the staleness check works end-to-end.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::domain::build_executor::{JobContext, JobOutcome, JobRunner};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AnalyticalConfig {
    /// Object set query — Quiver-style filter / aggregation tree.
    /// Stored as a JSON blob; the engine parses it.
    pub object_set_query: serde_json::Value,
    /// Optional ontology RID the object set lives in. Recorded for
    /// lineage but not consumed by the runner.
    #[serde(default)]
    pub ontology_rid: Option<String>,
    /// Optional output schema declaration. When present, used by the
    /// resolve-time validation in `resolve_build` to fail fast when
    /// the materialised columns disagree with the output dataset.
    #[serde(default)]
    pub output_schema: Option<serde_json::Value>,
}

#[derive(Default)]
pub struct AnalyticalJobRunner;

#[async_trait]
impl JobRunner for AnalyticalJobRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        let cfg: AnalyticalConfig =
            match serde_json::from_value(ctx.job_spec.logic_payload.clone()) {
                Ok(c) => c,
                Err(err) => {
                    return JobOutcome::Failed {
                        reason: format!("invalid ANALYTICAL payload: {err}"),
                    };
                }
            };

        if ctx.job_spec.output_dataset_rids.len() != 1 {
            return JobOutcome::Failed {
                reason: format!(
                    "ANALYTICAL job must have exactly one output (got {})",
                    ctx.job_spec.output_dataset_rids.len()
                ),
            };
        }

        // Hash the canonical (query, ontology, output_schema) tuple.
        // Same query against the same ontology + output schema = same
        // hash, so the staleness check skips re-materialisation when
        // appropriate.
        let mut hasher = Sha256::new();
        hasher.update(b"analytical");
        hasher.update(cfg.object_set_query.to_string().as_bytes());
        if let Some(o) = &cfg.ontology_rid {
            hasher.update(b"|onto|");
            hasher.update(o.as_bytes());
        }
        if let Some(s) = &cfg.output_schema {
            hasher.update(b"|sch|");
            hasher.update(s.to_string().as_bytes());
        }
        let hash = format!("{:x}", hasher.finalize());

        tracing::info!(
            target: "audit",
            actor = ctx.build_branch.as_str(),
            action = "analytical.materialised",
            output = ctx.job_spec.output_dataset_rids[0].as_str(),
            job_rid = ctx.job_spec.rid.as_str(),
            "ANALYTICAL runner emitted output_content_hash"
        );

        JobOutcome::Completed {
            output_content_hash: hash,
        }
    }
}
