//! `pipeline-build-service` — shared library used by the binary entry
//! point (`src/main.rs`) and the integration test crate
//! (`tests/*.rs`).
//!
//! Surfaces the lifecycle plumbing — `domain::build_resolution`,
//! `domain::job_lifecycle`, `models::build`, `models::job` — so tests
//! can drive `resolve_build` and `transition_job` against a real
//! Postgres without going through HTTP.

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;
use storage_abstraction::StorageBackend;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;
pub mod spark;

use crate::domain::build_executor::OutputTransactionClient;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub storage: Arc<dyn StorageBackend>,
    pub data_dir: String,
    pub dataset_service_url: String,
    pub workflow_service_url: String,
    pub ai_service_url: String,
    pub storage_backend: String,
    pub storage_bucket: String,
    pub s3_endpoint: Option<String>,
    pub s3_region: Option<String>,
    pub s3_access_key: Option<String>,
    pub s3_secret_key: Option<String>,
    pub local_storage_root: Option<String>,
    pub distributed_pipeline_workers: usize,
    pub distributed_compute_poll_interval_ms: u64,
    pub distributed_compute_timeout_secs: u64,
    pub lifecycle_ports: Option<handlers::builds_v1::BuildLifecyclePorts>,
    /// Kubernetes client used to POST `SparkApplication` CRs. `None`
    /// in unit tests / environments without a kubeconfig — the
    /// SparkApplication handlers respond with `503` in that case.
    pub kube_client: Option<kube::Client>,
    /// Namespace the Spark Operator watches.
    pub spark_namespace: String,
    /// Default `image:` for the SparkApplication CR.
    pub pipeline_runner_image: String,
    /// ADR-0041 — productive [`OutputTransactionClient`] for Foundry
    /// Iceberg outputs. `Some` when `FOUNDRY_ICEBERG_CATALOG_URL` is
    /// set at boot; `None` means the binary did not find a catalog
    /// and `main` already logged a warn so operators know that
    /// catalog-side row-locking + multi-table atomicity (ADR-0041 §
    /// Decision item 2) won't fire on Iceberg dataset commits.
    pub iceberg_output_client: Option<Arc<dyn OutputTransactionClient>>,
}
