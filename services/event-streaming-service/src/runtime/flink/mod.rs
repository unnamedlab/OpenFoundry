//! Apache Flink runtime integration (Bloque D).
//!
//! Pragmatic split:
//!
//! * [`sql`]      — pure DAG → Flink SQL emitter. No I/O, fully unit
//!                  tested. Available without any feature gate so the
//!                  REST handlers can render a preview.
//! * [`deployer`] — gated by `flink-runtime`. Uses `kube-rs` to create
//!                  the `FlinkDeployment` + `FlinkSessionJob` Custom
//!                  Resources that materialise the SQL on a Flink
//!                  Kubernetes Operator deployment.
//! * [`metrics_poller`] — gated by `flink-runtime`. Periodic
//!                  `tokio::interval` task that scrapes the JobManager
//!                  REST API (`:8081/jobs/.../metrics`) and writes a
//!                  `streaming_topology_runs` row with the canonical
//!                  KPI vector.
//! * [`job_graph`] — gated by `flink-runtime`. HTTP proxy used by the
//!                  REST handler `/topologies/:id/job-graph`.

pub mod sql;

use crate::models::stream::{StreamConsistency, StreamDefinition};
use crate::models::topology::TopologyDefinition;

/// Resolve whether a topology should be deployed with `EXACTLY_ONCE`
/// checkpointing — the strongest of the topology's own consistency
/// guarantee and any source stream's `pipeline_consistency`.
///
/// Per Foundry docs, "Streaming pipelines support both AT_LEAST_ONCE
/// and EXACTLY_ONCE". The stream record carries the operator's intent,
/// the topology carries the runtime's translation. When either side
/// asks for EXACTLY_ONCE the Flink job must run with EXACTLY_ONCE.
pub fn effective_exactly_once(topology: &TopologyDefinition, streams: &[StreamDefinition]) -> bool {
    if topology
        .consistency_guarantee
        .eq_ignore_ascii_case("exactly-once")
    {
        return true;
    }
    streams
        .iter()
        .filter(|s| topology.source_stream_ids.contains(&s.id))
        .any(|s| matches!(s.pipeline_consistency, StreamConsistency::ExactlyOnce))
}

#[cfg(feature = "flink-runtime")]
pub mod deployer;

#[cfg(feature = "flink-runtime")]
pub mod metrics_poller;

#[cfg(feature = "flink-runtime")]
pub mod job_graph;

/// Coordinates needed to address a Flink job from the control plane.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FlinkJobCoords {
    pub deployment_name: String,
    pub namespace: String,
    /// Runtime job id reported by the JobManager. `None` until the job
    /// reaches `RUNNING` and the metrics poller writes it back.
    pub job_id: Option<String>,
}

/// Configuration knobs for the Flink runtime, sourced from environment
/// variables. Held in [`crate::AppState`] so the deployer/poller/proxy
/// share a single source of truth.
#[derive(Debug, Clone)]
pub struct FlinkRuntimeConfig {
    /// Default namespace used when the topology does not pin one. Read
    /// from `FLINK_NAMESPACE` (env), defaulting to `flink`.
    pub default_namespace: String,
    /// Container image carrying the `sql-runner.jar`. Read from
    /// `FLINK_SQL_RUNNER_IMAGE`, defaulting to a documented placeholder.
    pub sql_runner_image: String,
    /// Flink runtime version label. Read from `FLINK_VERSION`,
    /// defaulting to `v1_19` (matches the operator pinned in
    /// `infra/runbooks/flink.md`).
    pub flink_version: String,
    /// JobManager REST endpoint template. `{deployment}` and
    /// `{namespace}` are substituted at call time. Defaults to the
    /// in-cluster service name pattern emitted by the operator.
    pub jobmanager_url_template: String,
    /// Interval at which `metrics_poller` scrapes each Flink job.
    pub metrics_poll_interval_ms: u64,
    /// S3 / Ceph URI under which checkpoints/savepoints land.
    pub state_bucket_uri: String,
}

impl FlinkRuntimeConfig {
    pub fn from_env() -> Self {
        Self {
            default_namespace: std::env::var("FLINK_NAMESPACE")
                .unwrap_or_else(|_| "flink".to_string()),
            sql_runner_image: std::env::var("FLINK_SQL_RUNNER_IMAGE").unwrap_or_else(|_| {
                "ghcr.io/unnamedlab/openfoundry/flink-sql-runner:1.19.1-0.1.0".to_string()
            }),
            flink_version: std::env::var("FLINK_VERSION").unwrap_or_else(|_| "v1_19".to_string()),
            jobmanager_url_template: std::env::var("FLINK_JOBMANAGER_URL_TEMPLATE")
                .unwrap_or_else(|_| "http://{deployment}-rest.{namespace}.svc:8081".to_string()),
            metrics_poll_interval_ms: std::env::var("FLINK_METRICS_POLL_INTERVAL_MS")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(15_000),
            state_bucket_uri: std::env::var("FLINK_STATE_BUCKET_URI")
                .unwrap_or_else(|_| "s3://openfoundry-iceberg/flink".to_string()),
        }
    }

    /// Resolve the JobManager URL for a given deployment.
    pub fn jobmanager_url(&self, deployment: &str, namespace: &str) -> String {
        self.jobmanager_url_template
            .replace("{deployment}", deployment)
            .replace("{namespace}", namespace)
    }
}

impl Default for FlinkRuntimeConfig {
    fn default() -> Self {
        Self::from_env()
    }
}
