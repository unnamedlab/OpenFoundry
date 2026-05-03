//! `pipeline-build-service` binary entry point.
//!
//! Build / execution side of Pipeline Builder. Shares the DAG executor and
//! lineage recorder with `pipeline-authoring-service` via `#[path]` shims;
//! exposes only the run-side surface (trigger, retry, list, get) plus the
//! scheduler dispatch endpoint.
//!
//! Maps to Foundry's "Build" controls from the Pipeline Builder UI:
//! "Build dataset" (single output), "Build downstream" (descendants),
//! "Schedules" (cron-driven runs delegated by `pipeline-schedule-service`).

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{get, post},
};
use sqlx::postgres::PgPoolOptions;
use storage_abstraction::StorageBackend;
use tracing_subscriber::EnvFilter;

use pipeline_build_service::{AppState, config::AppConfig, domain, handlers};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("pipeline_build_service=info,tower_http=info")),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;

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
        lifecycle_ports: None,
    };

    domain::metrics::init();

    sqlx::migrate!("./migrations").run(&state.db).await?;

    let runs = Router::new()
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
        .route("/builds", get(handlers::builds::list_builds))
        .route("/builds/_summary", get(handlers::builds::queue_summary))
        .route(
            "/builds/{run_id}/abort",
            post(handlers::builds::abort_build),
        )
        .route(
            "/pipelines/_scheduler/run-due",
            post(handlers::execute::run_due_scheduled_pipelines),
        )
        // P2 — dry-run-resolve. Pure simulation of compile_build_graph
        // + branch_resolution; no locks, no transactions opened.
        .route(
            "/pipelines/{pipeline_rid}/dry-run-resolve",
            post(handlers::dry_run::dry_run_resolve),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let builds_v1 = Router::new()
        .route(
            "/builds",
            post(handlers::builds_v1::create_build).get(handlers::builds_v1::list_builds_v1),
        )
        .route("/builds/{rid}", get(handlers::builds_v1::get_build))
        .route(
            "/builds/{rid}:abort",
            post(handlers::builds_v1::abort_build_v1),
        )
        .route(
            "/datasets/{rid}/builds",
            get(handlers::builds_v1::list_dataset_builds),
        )
        .route(
            "/jobs/{rid}/outputs",
            get(handlers::builds_v1::get_job_outputs),
        )
        .route(
            "/jobs/{rid}/input-resolutions",
            get(handlers::builds_v1::get_job_input_resolutions),
        )
        .route(
            "/job-specs/{kind}",
            post(handlers::builds_v1::create_job_spec),
        )
        // P4 — live logs surface.
        .route(
            "/jobs/{rid}/logs",
            get(handlers::job_logs::list_logs).post(handlers::job_logs::emit_log),
        )
        .route(
            "/jobs/{rid}/logs/stream",
            get(handlers::job_logs::stream_logs),
        )
        .route(
            "/jobs/{rid}/logs/ws",
            get(handlers::job_logs::ws_logs),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1/data-integration", runs)
        .nest("/v1", builds_v1)
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("pipeline-build-service listening on http://{}", addr);
    axum::serve(listener, app).await?;
    Ok(())
}

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
