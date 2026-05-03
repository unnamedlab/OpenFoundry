//! `TRANSFORM` job runner — Foundry "Code Repository / Pipeline
//! Builder transform".
//!
//! The actual SQL / Python / Polars / DataFusion execution lives in
//! `domain::engine::runtime`. This runner is a thin shim that
//! produces a deterministic [`JobOutcome`] from `JobSpec.content_hash`
//! so the parallel orchestrator and the staleness check share a
//! single source of truth (the spec hash).
//!
//! When the engine integration lands (next milestone) this module
//! grows the call into `engine::execute_node` plus marker propagation
//! and lineage emission. For now the contract is: TRANSFORM runner
//! always returns `Completed { output_content_hash = spec.content_hash }`,
//! mirroring P2's previous behaviour but routed through the new
//! dispatcher.

use async_trait::async_trait;

use crate::domain::build_executor::{JobContext, JobOutcome, JobRunner};

#[derive(Default)]
pub struct TransformJobRunner;

#[async_trait]
impl JobRunner for TransformJobRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        // The orchestrator already enforces multi-output atomicity
        // via job_outputs commits, so we just emit the canonical
        // hash. Engine wiring is intentionally out of scope here —
        // see `domain::engine::runtime` for the existing transform
        // surface.
        JobOutcome::Completed {
            output_content_hash: ctx.job_spec.content_hash.clone(),
        }
    }
}
