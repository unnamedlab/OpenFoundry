//! `pipeline-authoring-service` binary entry point.
//!
//! Authoring + execution control plane for Pipeline Builder.
//!
//! Mirrors the surface described in the Palantir Foundry docs
//! (`docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Applications/Pipeline Builder/`):
//! pipelines own a DAG of transform nodes (sql / python / llm / wasm /
//! passthrough), expose validate / compile / preview / deploy operations,
//! materialize each deploy as a `pipeline_run`, and surface run history.
//!
//! This service owns the WRITE path (create / update / delete pipelines and
//! trigger ad-hoc runs). `pipeline-build-service` reuses the same DAG
//! executor for builder-side preview-only routes, and
//! `pipeline-schedule-service` polls `pipelines.next_run_at` to dispatch
//! cron-driven runs back here.

mod config;
mod domain;
mod handlers;
mod models;

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{get, post},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use storage_abstraction::StorageBackend;
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

/// Shared state for every Axum handler. Field set is the union of what
/// `domain::executor` and `domain::engine::runtime` read; do not narrow
/// without auditing every consumer.
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
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("pipeline_authoring_service=info,tower_http=info")
            }),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::new();
    let storage = build_storage(&app_config)?;
    let state = AppState {
        db,
        jwt_config: jwt_config.clone(),
        http_client,
        storage,
        data_dir: app_config.data_dir.clone(),
        dataset_service_url: app_config.dataset_service_url.clone(),
        workflow_service_url: app_config.workflow_service_url.clone(),
        ai_service_url: app_config.ai_service_url.clone(),
        storage_backend: app_config.storage_backend.clone(),
        storage_bucket: app_config.storage_bucket.clone(),
        s3_endpoint: app_config.s3_endpoint.clone(),
        s3_region: app_config.s3_region.clone(),
        s3_access_key: app_config.s3_access_key.clone(),
        s3_secret_key: app_config.s3_secret_key.clone(),
        local_storage_root: app_config.local_storage_root.clone(),
        distributed_pipeline_workers: app_config.distributed_pipeline_workers,
        distributed_compute_poll_interval_ms: app_config.distributed_compute_poll_interval_ms,
        distributed_compute_timeout_secs: app_config.distributed_compute_timeout_secs,
    };

    // Pipeline Builder authoring surface (Foundry: Pipeline Builder app).
    // CRUD + Validate / Compile / Prune live here; run execution is owned
    // by `pipeline-build-service` (Foundry: Builds app) and scheduling by
    // `pipeline-schedule-service` (Foundry: Schedules). The canvas at
    // `apps/web/src/lib/components/pipeline/PipelineCanvas.svelte`
    // serializes the in-flight graph as `nodes: PipelineNode[]` and POSTs
    // it here.
    let pipelines = Router::new()
        // CRUD over `pipelines` rows (`dag` is JSONB of PipelineNode[]).
        .route(
            "/pipelines",
            get(handlers::crud::list_pipelines).post(handlers::crud::create_pipeline),
        )
        .route(
            "/pipelines/{id}",
            get(handlers::crud::get_pipeline)
                .put(handlers::crud::update_pipeline)
                .delete(handlers::crud::delete_pipeline),
        )
        // Validate + compile + prune accept an in-flight graph (no persisted
        // pipeline required). Mirrors Foundry's "Validate" / "Preview" buttons
        // shown in the canvas toolbar before Deploy.
        .route("/pipelines/_validate", post(handlers::compiler::validate_pipeline))
        .route("/pipelines/_compile", post(handlers::compiler::compile_pipeline))
        .route("/pipelines/_prune", post(handlers::compiler::prune_pipeline))
        // P2 — JobSpec publish/list. Foundry doc § "Job graph compilation":
        // each commit on a Code-Repo branch publishes one JobSpec per
        // output dataset; builds resolve them by walking the branch
        // fallback chain.
        .route(
            "/pipelines/{pipeline_rid}/branches/{branch}/job-specs",
            get(handlers::job_specs::list_by_pipeline_branch)
                .post(handlers::job_specs::publish_job_spec),
        )
        .route(
            "/datasets/{dataset_rid}/job-specs",
            get(handlers::job_specs::list_by_dataset),
        )
        // P4 — Parameterized pipelines. Per the Foundry doc the surface
        // is intentionally narrow: enable, then per-deployment CRUD +
        // a `:run` endpoint that rejects non-manual triggers.
        .route(
            "/pipelines/{rid}/parameterized",
            post(handlers::parameterized::enable_parameterized),
        )
        .route(
            "/parameterized-pipelines/{id}/deployments",
            post(handlers::parameterized::create_deployment)
                .get(handlers::parameterized::list_deployments),
        )
        .route(
            "/parameterized-pipelines/{id}/deployments/{dep_id}",
            axum::routing::delete(handlers::parameterized::delete_deployment),
        )
        .route(
            "/parameterized-pipelines/{id}/deployments/{dep_id}:run",
            post(handlers::parameterized::run_deployment),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1/data-integration", pipelines)
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("pipeline-authoring-service listening on http://{}", addr);
    axum::serve(listener, app).await?;
    Ok(())
}

/// Construct the configured object-storage backend.
///
/// `s3` for S3/MinIO (requires region + access/secret); anything else falls
/// back to the local filesystem rooted at `local_storage_root` or `data_dir`.
fn build_storage(cfg: &AppConfig) -> Result<Arc<dyn StorageBackend>, Box<dyn std::error::Error>> {
    if cfg.storage_backend.eq_ignore_ascii_case("s3") {
        let region = cfg.s3_region.as_deref().unwrap_or("us-east-1");
        let access = cfg.s3_access_key.as_deref().unwrap_or_default();
        let secret = cfg.s3_secret_key.as_deref().unwrap_or_default();
        let s3 = storage_abstraction::s3::S3Storage::new(
            &cfg.storage_bucket,
            region,
            cfg.s3_endpoint.as_deref(),
            access,
            secret,
        )?;
        Ok(Arc::new(s3))
    } else {
        let root = cfg
            .local_storage_root
            .clone()
            .unwrap_or_else(|| cfg.data_dir.clone());
        std::fs::create_dir_all(&root).ok();
        Ok(Arc::new(storage_abstraction::local::LocalStorage::new(
            &root,
        )?))
    }
}
