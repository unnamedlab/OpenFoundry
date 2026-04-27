#![allow(dead_code)]

mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;
use storage_abstraction::StorageBackend;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub dataset_service_url: String,
    pub workflow_service_url: String,
    pub ai_service_url: String,
    pub storage: std::sync::Arc<dyn StorageBackend>,
    pub storage_backend: String,
    pub storage_bucket: String,
    pub s3_endpoint: Option<String>,
    pub s3_region: Option<String>,
    pub local_storage_root: Option<String>,
    pub distributed_pipeline_workers: usize,
    pub distributed_compute_poll_interval_ms: u64,
    pub distributed_compute_timeout_secs: u64,
}

#[tokio::main]
async fn main() {
    observability::init_tracing("pipeline-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("failed to run migrations");

    let _migration_owner_state = AppState {
        db: pool,
        jwt_config: JwtConfig::new(&cfg.jwt_secret).with_env_defaults(),
        http_client: reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(60))
            .build()
            .expect("failed to build HTTP client"),
        dataset_service_url: cfg.dataset_service_url.clone(),
        workflow_service_url: cfg.workflow_service_url.clone(),
        ai_service_url: cfg.ai_service_url.clone(),
        storage_backend: cfg.storage_backend.clone(),
        storage_bucket: cfg.storage_bucket.clone(),
        s3_endpoint: cfg.s3_endpoint.clone(),
        s3_region: cfg.s3_region.clone(),
        local_storage_root: cfg.local_storage_root.clone(),
        distributed_pipeline_workers: cfg.distributed_pipeline_workers.max(1),
        distributed_compute_poll_interval_ms: cfg.distributed_compute_poll_interval_ms.max(250),
        distributed_compute_timeout_secs: cfg.distributed_compute_timeout_secs.max(30),
        storage: std::sync::Arc::new(
            storage_abstraction::local::LocalStorage::new(
                cfg.local_storage_root
                    .as_deref()
                    .unwrap_or("/tmp/openfoundry-pipeline-shell"),
            )
            .expect("failed to init migration-owner local storage"),
        ),
    };

    let app = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("pipeline-service")) }),
    );

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting pipeline-service on {addr}");
    tracing::info!("pipeline-service now acts as migration owner and compatibility shell");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
