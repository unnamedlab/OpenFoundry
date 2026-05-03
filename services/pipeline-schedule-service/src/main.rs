//! `pipeline-schedule-service` binary entry point.
//!
//! ## Status: post-S2.4 substrate (Temporal Schedules)
//!
//! Per Stream **S2.4** of
//! `docs/architecture/migration-plan-cassandra-foundry-parity.md` and
//! ADR-0021, the in-process **tick loop** that polled
//! `pipelines.next_run_at` / `workflow_definitions.next_run_at` and
//! fired runs from this binary has been **removed**. Cron-driven and
//! event-driven runs are now dispatched by **Temporal Schedules**
//! through the typed [`temporal_client::PipelineScheduleClient`]
//! facade in [`crate::domain::temporal_schedule`]. Temporal owns:
//!   - exactly-once dispatch under N replicas (no row-locking races);
//!   - durable cron evaluation (no in-process clock skew);
//!   - HA failover (the schedule survives any worker restart).
//!
//! What stays here:
//!   - REST CRUD over schedule **definitions** (Postgres remains the
//!     declarative source of truth, as called out in S2.4.b).
//!   - Backfill / preview helpers that compute windows for the UI.
//!
//! What does **not** stay here:
//!   - `tokio::spawn`-based ticker (deleted, was 25 LOC in `main`).
//!   - In-process pipeline executor (delegated to the worker
//!     in `workers-go/pipeline/`, which registers the `PipelineRun`
//!     workflow type and is what every Temporal Schedule fires).
//!   - The legacy break-glass admin endpoints that polled
//!     `next_run_at` from Postgres (removed once every persisted
//!     schedule had been migrated to Temporal Schedules; cron
//!     dispatch now belongs entirely to Temporal).

mod config;
mod domain;
mod handlers;
mod models;
#[path = "../../lineage-service/src/query_router.rs"]
mod query_router;

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{get, post},
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use storage_abstraction::StorageBackend;
use temporal_client::PipelineScheduleClient;
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;

/// Shared state.
///
/// **Field set frozen** — the path-pulled `engine::runtime` tests
/// from `pipeline-authoring-service` construct this struct
/// positionally, so adding fields here would break that test target.
/// The Temporal client is therefore injected as an Axum `Extension`
/// instead of as a state field; see [`Extension<PipelineScheduleClient>`]
/// extractors in [`crate::handlers::temporal_schedule`].
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub lineage_runtime: domain::lineage::SharedLineageRuntimeStore,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub storage: Arc<dyn StorageBackend>,
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
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("pipeline_schedule_service=info,tower_http=info")
            }),
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
    let lineage_runtime =
        domain::lineage::LineageRuntimeStore::build(domain::lineage::LineageRuntimeStoreConfig {
            cassandra_contact_points: app_config
                .cassandra_contact_points
                .split(',')
                .map(|value| value.trim().to_string())
                .filter(|value| !value.is_empty())
                .collect(),
            cassandra_local_dc: app_config.cassandra_local_dc.clone(),
        })
        .await?;

    // Temporal wiring. When `TEMPORAL_HOST_PORT` is present this is a
    // gRPC client; otherwise the local dry-run client keeps smoke
    // tests deterministic. The namespace is the one provisioned by
    // `infra/k8s/platform/manifests/temporal/` (S2.1.b).
    let (workflow_client, temporal_namespace) =
        temporal_client::runtime_workflow_client("pipeline-schedule-service").await?;
    let pipeline_schedule_client = PipelineScheduleClient::new(workflow_client, temporal_namespace);

    let state = AppState {
        db,
        lineage_runtime,
        jwt_config: jwt_config.clone(),
        http_client,
        storage,
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

    // S2.4.a — the in-process tick loop has been removed. Temporal
    // Schedules (created via `pipeline_schedule_client.create`) own
    // exactly-once dispatch from now on. The legacy break-glass
    // REST endpoints that polled `next_run_at` from Postgres have
    // also been removed now that the migration is complete.

    // Schedule + workflow surface. Auth is enforced by the layered middleware
    // (matches the `_user: AuthUser` extractors in every handler).
    let scheduler = Router::new()
        // ---- new Foundry-parity surface (P1 of the trigger redesign) ----
        .route(
            "/v1/schedules",
            post(handlers::schedules_v2::create_schedule)
                .get(handlers::schedules_v2::list_schedules),
        )
        .route(
            "/v1/schedules/{rid}",
            get(handlers::schedules_v2::get_schedule)
                .patch(handlers::schedules_v2::patch_schedule)
                .delete(handlers::schedules_v2::delete_schedule),
        )
        .route(
            "/v1/schedules/{rid}:run-now",
            post(handlers::schedules_v2::run_now),
        )
        .route(
            "/v1/schedules/{rid}:pause",
            post(handlers::schedules_v2::pause_schedule),
        )
        .route(
            "/v1/schedules/{rid}:resume",
            post(handlers::schedules_v2::resume_schedule),
        )
        .route(
            "/v1/schedules/{rid}:exempt-from-auto-pause",
            post(handlers::schedules_v2::set_exempt_from_auto_pause),
        )
        .route(
            "/v1/schedules/{rid}:convert-to-project-scope",
            post(handlers::schedules_v2::convert_to_project_scope),
        )
        .route(
            "/v1/schedules/{rid}/runs",
            get(handlers::schedules_v2::list_runs),
        )
        .route(
            "/v1/schedules/{rid}/versions",
            get(handlers::schedules_v2::list_versions),
        )
        .route(
            "/v1/schedules/{rid}/versions/diff",
            get(handlers::schedules_v2::version_diff),
        )
        .route(
            "/v1/schedules/{rid}/versions/{n}",
            get(handlers::schedules_v2::get_version),
        )
        .route(
            "/v1/schedules/{rid}/preview-next-fires",
            get(handlers::schedules_v2::preview_next_fires),
        )
        // ---- legacy preview / backfill (kept alongside) -----------------
        .route("/schedules/due", get(handlers::schedule::list_due_runs))
        .route("/schedules/preview", post(handlers::schedule::preview_windows))
        .route("/schedules/backfill", post(handlers::schedule::backfill_runs))
        .route(
            "/schedules/temporal",
            post(handlers::temporal_schedule::create_schedule),
        )
        .route(
            "/schedules/temporal/{schedule_id}",
            axum::routing::delete(handlers::temporal_schedule::delete_schedule),
        )
        .route(
            "/workflows/events/{event_name}",
            post(handlers::workflow::trigger_event),
        )
        // Sweep linter (P3) — see `Linter/Sweep schedules.md`.
        .route(
            "/v1/scheduling-linter/sweep",
            post(handlers::linter::sweep),
        )
        .route(
            "/v1/scheduling-linter/sweep:apply",
            post(handlers::linter::apply),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1/data-integration", scheduler)
        .route("/healthz", get(|| async { "ok" }))
        .layer(axum::Extension(pipeline_schedule_client))
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("pipeline-schedule-service listening on http://{}", addr);
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
