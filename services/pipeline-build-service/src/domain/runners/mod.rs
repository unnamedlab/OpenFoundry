//! Job runners — one per logic kind enumerated in Foundry
//! Builds.md § Jobs and JobSpecs.
//!
//!   * `SYNC`         — Data Connection ingest jobs (calls
//!     `connector-management-service`).
//!   * `TRANSFORM`    — Pipeline Builder / Code Repository transforms
//!     (delegates to the existing `domain::engine` DataFusion / Polars /
//!     Python runtime).
//!   * `HEALTH_CHECK` — emits a check result to
//!     `dataset-quality-service`.
//!   * `ANALYTICAL`   — materialises an object-set query into the
//!     output dataset.
//!   * `EXPORT`       — pushes the input dataset to an external
//!     destination (S3 / GCS / HTTP / JDBC).
//!
//! All five funnel through the same [`build_executor::JobRunner`]
//! interface so the parallel orchestrator (P2) does not need to know
//! the kind of work it is driving. The actual per-kind dispatch lives
//! in [`DispatchingRunner`].

use std::sync::Arc;

use async_trait::async_trait;

use crate::domain::build_executor::{JobContext, JobOutcome, JobRunner};

pub mod analytical;
pub mod export;
pub mod health_check;
pub mod resolver;
pub mod sync;
pub mod transform;
pub mod view_filter;

pub use analytical::{AnalyticalConfig, AnalyticalJobRunner};
pub use export::{ExportConfig, ExportJobRunner, ExportTarget};
pub use health_check::{HealthCheckConfig, HealthCheckJobRunner, HealthCheckKind};
pub use resolver::{ViewResolutionOutcome, resolve_view_filters};
pub use sync::{SyncConfig, SyncJobRunner};
pub use transform::TransformJobRunner;
pub use view_filter::{ResolvedViewFilter, ViewFilter};

/// Canonical names of the five Foundry logic kinds. Use these constants
/// instead of hard-coding the strings so renames are caught at compile
/// time. Stored in `JobSpec.logic_kind`.
pub mod logic_kinds {
    pub const SYNC: &str = "SYNC";
    pub const TRANSFORM: &str = "TRANSFORM";
    pub const HEALTH_CHECK: &str = "HEALTH_CHECK";
    pub const ANALYTICAL: &str = "ANALYTICAL";
    pub const EXPORT: &str = "EXPORT";

    pub const ALL: &[&str] = &[SYNC, TRANSFORM, HEALTH_CHECK, ANALYTICAL, EXPORT];

    pub fn is_known(kind: &str) -> bool {
        ALL.iter().any(|k| *k == kind)
    }
}

/// Routes each [`JobContext`] to the matching kind-specific runner.
///
/// The dispatch is a thin lookup; every runner implements the same
/// [`JobRunner`] trait so the parallel orchestrator keeps a single
/// handle. Unknown `logic_kind`s fail fast with a `Failed` outcome.
pub struct DispatchingRunner {
    pub sync: Arc<dyn JobRunner>,
    pub transform: Arc<dyn JobRunner>,
    pub health_check: Arc<dyn JobRunner>,
    pub analytical: Arc<dyn JobRunner>,
    pub export: Arc<dyn JobRunner>,
}

impl DispatchingRunner {
    /// Convenience: build a dispatcher with the production runner set
    /// (HTTP-backed). For tests, construct the struct manually with
    /// mocked runners.
    pub fn from_clients(
        connector_base_url: String,
        quality_base_url: String,
        http: reqwest::Client,
    ) -> Self {
        Self {
            sync: Arc::new(SyncJobRunner::new(connector_base_url, http.clone())),
            transform: Arc::new(TransformJobRunner::default()),
            health_check: Arc::new(HealthCheckJobRunner::new(quality_base_url, http.clone())),
            analytical: Arc::new(AnalyticalJobRunner::default()),
            export: Arc::new(ExportJobRunner::new(http)),
        }
    }
}

#[async_trait]
impl JobRunner for DispatchingRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        match ctx.job_spec.logic_kind.as_str() {
            logic_kinds::SYNC => self.sync.run(ctx).await,
            logic_kinds::TRANSFORM => self.transform.run(ctx).await,
            logic_kinds::HEALTH_CHECK => self.health_check.run(ctx).await,
            logic_kinds::ANALYTICAL => self.analytical.run(ctx).await,
            logic_kinds::EXPORT => self.export.run(ctx).await,
            other => JobOutcome::Failed {
                reason: format!("unknown logic_kind: {other}"),
            },
        }
    }
}

/// Minimal validation a JobSpec must pass before resolve_build accepts
/// it. Foundry doc:
///
///   * Sync         — must declare exactly the dataset(s) the source
///     writes to.
///   * Transform    — at least one output (current code path).
///   * HealthCheck  — exactly one output, the dataset under check.
///   * Analytical   — exactly one output (object set materialised
///     into a dataset).
///   * Export       — outputs MAY be empty (data leaves Foundry).
pub fn validate_logic_kind(logic_kind: &str, output_count: usize) -> Result<(), String> {
    match logic_kind {
        logic_kinds::SYNC => {
            if output_count == 0 {
                return Err("SYNC job must declare at least one output dataset".into());
            }
        }
        logic_kinds::TRANSFORM => {
            if output_count == 0 {
                return Err("TRANSFORM job must declare at least one output dataset".into());
            }
        }
        logic_kinds::HEALTH_CHECK => {
            if output_count != 1 {
                return Err(format!(
                    "HEALTH_CHECK job must declare exactly one output dataset (got {output_count})"
                ));
            }
        }
        logic_kinds::ANALYTICAL => {
            if output_count != 1 {
                return Err(format!(
                    "ANALYTICAL job must declare exactly one output dataset (got {output_count})"
                ));
            }
        }
        logic_kinds::EXPORT => {
            // Export pushes data outside Foundry; an output dataset is
            // optional (often zero).
        }
        other => return Err(format!("unknown logic_kind: {other}")),
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_logic_kind_enforces_output_arity() {
        assert!(validate_logic_kind(logic_kinds::SYNC, 0).is_err());
        assert!(validate_logic_kind(logic_kinds::SYNC, 1).is_ok());
        assert!(validate_logic_kind(logic_kinds::HEALTH_CHECK, 0).is_err());
        assert!(validate_logic_kind(logic_kinds::HEALTH_CHECK, 1).is_ok());
        assert!(validate_logic_kind(logic_kinds::HEALTH_CHECK, 2).is_err());
        assert!(validate_logic_kind(logic_kinds::ANALYTICAL, 1).is_ok());
        assert!(validate_logic_kind(logic_kinds::ANALYTICAL, 2).is_err());
        assert!(validate_logic_kind(logic_kinds::EXPORT, 0).is_ok());
        assert!(validate_logic_kind(logic_kinds::EXPORT, 2).is_ok());
        assert!(validate_logic_kind("FOO", 1).is_err());
    }

    #[test]
    fn known_kinds_are_recognised() {
        for k in logic_kinds::ALL {
            assert!(logic_kinds::is_known(k));
        }
        assert!(!logic_kinds::is_known("PRESENT"));
    }
}
