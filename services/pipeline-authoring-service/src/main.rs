#![allow(dead_code)]

mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    extract::FromRef,
    middleware,
    routing::{delete, get, post, put},
};
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

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("pipeline-authoring-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(60))
        .build()
        .expect("failed to build HTTP client");
    let storage = build_storage_backend(&cfg);

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        dataset_service_url: cfg.dataset_service_url.clone(),
        workflow_service_url: cfg.workflow_service_url.clone(),
        ai_service_url: cfg.ai_service_url.clone(),
        storage,
        storage_backend: cfg.storage_backend.clone(),
        storage_bucket: cfg.storage_bucket.clone(),
        s3_endpoint: cfg.s3_endpoint.clone(),
        s3_region: cfg.s3_region.clone(),
        local_storage_root: cfg.local_storage_root.clone(),
        distributed_pipeline_workers: cfg.distributed_pipeline_workers.max(1),
        distributed_compute_poll_interval_ms: cfg.distributed_compute_poll_interval_ms.max(250),
        distributed_compute_timeout_secs: cfg.distributed_compute_timeout_secs.max(30),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("pipeline-authoring-service")) }),
    );

    let protected = Router::new()
        .route("/api/v1/pipelines", post(handlers::crud::create_pipeline))
        .route("/api/v1/pipelines", get(handlers::crud::list_pipelines))
        .route("/api/v1/pipelines/{id}", get(handlers::crud::get_pipeline))
        .route(
            "/api/v1/pipelines/{id}",
            put(handlers::crud::update_pipeline),
        )
        .route(
            "/api/v1/pipelines/{id}",
            delete(handlers::crud::delete_pipeline),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::auth_layer,
        ));

    let app = Router::new()
        .merge(public)
        .merge(protected)
        .with_state(state);

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting pipeline-authoring-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}

fn build_storage_backend(cfg: &config::AppConfig) -> std::sync::Arc<dyn StorageBackend> {
    match cfg.storage_backend.as_str() {
        "local" => {
            let root = cfg
                .local_storage_root
                .as_deref()
                .unwrap_or("/tmp/of-datasets");
            std::sync::Arc::new(
                storage_abstraction::local::LocalStorage::new(root)
                    .expect("failed to init local storage"),
            )
        }
        "s3" => {
            let access_key = cfg
                .s3_access_key
                .as_deref()
                .expect("s3_access_key must be configured when storage_backend=s3");
            let secret_key = cfg
                .s3_secret_key
                .as_deref()
                .expect("s3_secret_key must be configured when storage_backend=s3");

            std::sync::Arc::new(
                storage_abstraction::s3::S3Storage::new(
                    &cfg.storage_bucket,
                    cfg.s3_region.as_deref().unwrap_or("us-east-1"),
                    cfg.s3_endpoint.as_deref(),
                    access_key,
                    secret_key,
                )
                .expect("failed to init S3 storage"),
            )
        }
        other => panic!("unsupported storage backend '{other}'"),
    }
}
