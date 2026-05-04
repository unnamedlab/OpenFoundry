//! `pipeline-schedule-service` binary entry point.
//!
//! ## Status: post-Tarea 3.5 substrate (cron-emitter)
//!
//! Per Tarea 3.5 of
//! `docs/architecture/migration-plan-foundry-pattern-orchestration.md`
//! and ADR-0037, the in-process **tick loop** that polled
//! `pipelines.next_run_at` / `workflow_definitions.next_run_at` and
//! fired runs from this binary has been **removed**, *and* the prior
//! Temporal-Schedules adapter that briefly owned cron dispatch in
//! S2.4 has been replaced. Cron-driven runs are now dispatched by the
//! **`schedules-tick`** Kubernetes `CronJob` (binary from
//! `libs/event-scheduler`), which scans
//! [`schedules.definitions`](crate::domain::cron_registrar) every
//! minute and publishes one Kafka event per due row. The handlers in
//! this service write the rows via
//! [`crate::domain::cron_registrar::CronRegistrar`].
//!
//! What stays here:
//!   - REST CRUD over schedule **definitions** (Postgres remains the
//!     declarative source of truth).
//!   - Backfill / preview helpers that compute windows for the UI.
//!
//! What does **not** stay here:
//!   - `tokio::spawn`-based ticker (deleted).
//!   - In-process pipeline executor (delegated to
//!     `pipeline-build-service` consuming `pipeline.scheduled.v1`).
//!   - The `temporal-client` adapter and the `/schedules/temporal`
//!     break-glass endpoints (removed alongside ADR-0037 — cron
//!     dispatch belongs to the `schedules-tick` CronJob now).

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
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;
use crate::domain::cron_registrar::CronRegistrar;

/// Shared state.
///
/// The path-pulled `engine::runtime` tests from
/// `pipeline-authoring-service` use this crate's local
/// `test_support::pipeline_runtime_app_state`, so this state shape can
/// keep the scheduler-only runtime fields without forcing the authoring
/// service to grow matching fields.
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

#[cfg(test)]
pub(crate) mod test_support {
    use std::sync::Arc;

    use sqlx::postgres::PgPoolOptions;
    use storage_abstraction::local::LocalStorage;
    use uuid::Uuid;

    use crate::{AppState, domain};

    pub(crate) fn pipeline_runtime_app_state() -> AppState {
        let storage_root = std::env::temp_dir().join(format!(
            "openfoundry-pipeline-runtime-tests-{}",
            Uuid::now_v7()
        ));
        let storage_root_str = storage_root.to_string_lossy().to_string();
        std::fs::create_dir_all(&storage_root).expect("test storage directory should exist");

        AppState {
            db: PgPoolOptions::new()
                .connect_lazy("postgres://postgres:postgres@localhost/openfoundry")
                .expect("lazy pool should build"),
            lineage_runtime: Arc::new(domain::lineage::LineageRuntimeStore::Memory(
                domain::lineage::MemoryLineageRuntimeStore::new(),
            )),
            jwt_config: auth_middleware::jwt::JwtConfig::generate().with_env_defaults(),
            http_client: reqwest::Client::new(),
            storage: Arc::new(
                LocalStorage::new(&storage_root_str).expect("local storage should initialize"),
            ),
            nats_url: "nats://localhost:4222".to_string(),
            data_dir: storage_root_str.clone(),
            dataset_service_url: "http://dataset.local".to_string(),
            workflow_service_url: "http://workflow.local".to_string(),
            ai_service_url: "http://ai.local".to_string(),
            storage_backend: "s3".to_string(),
            storage_bucket: "datasets".to_string(),
            s3_endpoint: Some("http://minio.local".to_string()),
            s3_region: Some("us-east-1".to_string()),
            s3_access_key: None,
            s3_secret_key: None,
            local_storage_root: Some(storage_root_str),
            distributed_pipeline_workers: 1,
            distributed_compute_poll_interval_ms: 5_000,
            distributed_compute_timeout_secs: 900,
        }
    }
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

    // Tarea 3.5 — cron dispatch is owned by the `schedules-tick`
    // K8s `CronJob` (binary from `libs/event-scheduler`). The
    // service writes to `schedules.definitions` via the registrar,
    // which the runner then drains every minute over Kafka.
    let cron_registrar = CronRegistrar::new(db.clone());

    // P5 — AIP-assisted schedule generation. `LLM_CATALOG_URL` points
    // the production HTTP client at llm-catalog-service; tests inject
    // their own `Arc<dyn LlmClient>` via the `Extension` layer.
    let llm_url = std::env::var("LLM_CATALOG_URL")
        .unwrap_or_else(|_| "http://llm-catalog:50000".to_string());
    let llm_client: std::sync::Arc<dyn domain::aip::LlmClient> = std::sync::Arc::new(
        domain::aip_http_client::HttpLlmClient::new(llm_url, reqwest::Client::new()),
    );

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

    // S2.4.a / Tarea 3.5 — the in-process tick loop and Temporal
    // Schedules adapter have both been removed. Cron dispatch is now
    // owned by the `schedules-tick` K8s `CronJob` reading from
    // `schedules.definitions` (see [`CronRegistrar`]).

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
        // ---- P5 — AIP + troubleshooting ---------------------------------
        .route(
            "/v1/schedules/aip:generate",
            post(handlers::aip::generate),
        )
        .route(
            "/v1/schedules/aip:explain",
            post(handlers::aip::explain),
        )
        .route(
            "/v1/schedules/{rid}/troubleshoot",
            get(handlers::troubleshoot::get_report),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1/data-integration", scheduler)
        .route("/healthz", get(|| async { "ok" }))
        .layer(axum::Extension(cron_registrar))
        .layer(axum::Extension(llm_client))
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
