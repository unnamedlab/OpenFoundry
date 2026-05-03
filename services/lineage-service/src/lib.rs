//! `lineage-service` — HTTP lineage APIs plus Kafka → Iceberg
//! materialisation for `lineage.events.v1`.

pub mod config;
pub mod domain;
pub mod handlers;
pub mod iceberg_schema;
pub mod kafka_to_iceberg;
pub mod models;
pub mod query_router;
pub mod runtime;

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;
use storage_abstraction::StorageBackend;

/// Shared state consumed by the lineage runtime modules.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub lineage_runtime: domain::lineage::SharedLineageRuntimeStore,
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
}
