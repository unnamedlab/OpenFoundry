//! `pipeline-schedule-service` binary entry point.
//!
//! Cron + event scheduler for both pipelines and workflows. Implements the
//! "Schedules" surface from Foundry Pipeline Builder + Action / Workflow
//! schedules: lists due runs, previews next windows, backfills historical
//! windows, and runs an in-process ticker that polls
//! `pipelines.next_run_at` / `workflow_definitions.next_run_at` and
//! dispatches them. Cron-driven workflow runs are emitted as
//! `WorkflowRunRequested` events on NATS; pipeline runs are executed
//! in-process via the shared `domain::executor` shim from
//! `pipeline-authoring-service`.
//!
//! The ticker cadence is bounded by `scheduler_tick_interval_secs`
//! (default 30s); this matches Foundry's "minute-resolution cron" guidance.

mod config;
mod domain;
mod handlers;
mod models;

use std::net::SocketAddr;
use std::time::Duration;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    middleware,
    routing::{get, post},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

/// Shared state. Field set covers what the shimmed `executor`,
/// `engine::runtime` and `lineage` modules read PLUS `nats_url` for the
/// NATS publisher in `domain::workflow`.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub nats_url: String,
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
            EnvFilter::new("pipeline_schedule_service=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();

    let state = AppState {
        db,
        jwt_config: jwt_config.clone(),
        nats_url: app_config.nats_url.clone(),
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

    // Background ticker: fires every `tick_secs` to dispatch any
    // pipeline/workflow whose `next_run_at <= now()`.
    //
    // We delegate to `domain::executor::run_due_scheduled_pipelines` and
    // `domain::workflow::run_due_cron_workflows`; both are idempotent
    // (next_run_at is advanced atomically before execution) so concurrent
    // replicas only race on the row lock without double-firing.
    let tick_secs = std::env::var("SCHEDULER_TICK_INTERVAL_SECS")
        .ok()
        .and_then(|s| s.parse::<u64>().ok())
        .unwrap_or(30);
    {
        let ticker_state = state.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_secs(tick_secs));
            interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
            loop {
                interval.tick().await;
                match domain::executor::run_due_scheduled_pipelines(&ticker_state).await {
                    Ok(0) => {}
                    Ok(n) => tracing::info!(triggered = n, "scheduler dispatched pipeline runs"),
                    Err(e) => tracing::warn!(error = %e, "scheduler pipeline tick failed"),
                }
                match domain::workflow::run_due_cron_workflows(&ticker_state).await {
                    Ok(0) => {}
                    Ok(n) => tracing::info!(triggered = n, "scheduler dispatched workflow runs"),
                    Err(e) => tracing::warn!(error = %e, "scheduler workflow tick failed"),
                }
            }
        });
    }

    // Schedule + workflow surface. Auth is enforced by the layered middleware
    // (matches the `_user: AuthUser` extractors in every handler).
    let scheduler = Router::new()
        .route("/schedules/due", get(handlers::schedule::list_due_runs))
        .route("/schedules/preview", post(handlers::schedule::preview_windows))
        .route("/schedules/backfill", post(handlers::schedule::backfill_runs))
        .route(
            "/workflows/_scheduler/run-due",
            post(handlers::workflow::run_due_cron_workflows),
        )
        .route(
            "/workflows/events/{event_name}",
            post(handlers::workflow::trigger_event),
        )
        // Manual dispatch trigger for ops / debugging (mirrors
        // pipeline-build-service /pipelines/_scheduler/run-due).
        .route(
            "/pipelines/_scheduler/run-due",
            post(handlers::execute::run_due_scheduled_pipelines),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1/data-integration", scheduler)
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("pipeline-schedule-service listening on http://{}", addr);
    axum::serve(listener, app).await?;
    Ok(())
}
