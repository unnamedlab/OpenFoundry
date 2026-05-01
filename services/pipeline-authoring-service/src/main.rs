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

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    middleware,
    routing::{delete, get, post, put},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

/// Shared state for every Axum handler. Field set is the union of what
/// `domain::executor`, `domain::engine::runtime` and `domain::lineage`
/// (shimmed from `lineage-service`) read; do not narrow without auditing
/// every consumer.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
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
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("pipeline_authoring_service=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();

    let state = AppState {
        db,
        jwt_config: jwt_config.clone(),
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

    // Pipeline Builder app surface. Path scheme matches
    // `apps/web/src/lib/api/pipelines.ts` so the Svelte canvas can
    // serialize the graph as `nodes: PipelineNode[]` and POST it here.
    let pipelines = Router::new()
        // Authoring (CRUD over `pipelines` rows; `dag` is JSONB of PipelineNode[])
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
        // Validate + compile + prune. These do NOT require a persisted pipeline;
        // they accept the in-flight graph from the canvas (Foundry's
        // "Validate / Preview" buttons before Deploy).
        .route("/pipelines/_validate", post(handlers::compiler::validate_pipeline))
        .route("/pipelines/_compile", post(handlers::compiler::compile_pipeline))
        .route("/pipelines/_prune", post(handlers::compiler::prune_pipeline))
        // Run lifecycle (ad-hoc trigger + retry from a node + run history).
        // Mirrors Foundry's "Deploy", "Build dataset", "Build downstream".
        .route(
            "/pipelines/{id}/runs",
            get(handlers::runs::list_runs).post(handlers::execute::trigger_run),
        )
        .route(
            "/pipelines/{id}/runs/{run_id}",
            get(handlers::runs::get_run),
        )
        .route(
            "/pipelines/{id}/runs/{run_id}/retry",
            post(handlers::execute::retry_run),
        )
        // Internal: invoked by `pipeline-schedule-service` ticker.
        .route(
            "/pipelines/_scheduler/run-due",
            post(handlers::execute::run_due_scheduled_pipelines),
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
