mod config;
#[allow(dead_code)]
mod domain;
#[allow(dead_code)]
mod handlers;
#[allow(dead_code)]
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;
use storage_abstraction::StorageBackend;

/// Shared application state passed to all handlers.
#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub storage: std::sync::Arc<dyn StorageBackend>,
    pub http_client: reqwest::Client,
    pub dataset_quality_service_url: String,
}

impl axum::extract::FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("dataset-service");

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

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let storage = build_storage_backend(&cfg);
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .expect("failed to build dataset HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        storage,
        http_client,
        dataset_quality_service_url: cfg.dataset_quality_service_url.clone(),
    };

    let public = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("dataset-service")) }),
        )
        .route(
            "/internal/datasets/{id}/metadata",
            get(handlers::internal::get_dataset_metadata),
        );

    let app = Router::new().merge(public).with_state(state);

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting dataset-service on {addr}");

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
