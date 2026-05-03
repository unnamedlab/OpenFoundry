//! `dataset-quality-service` binary.
//!
//! P6 — first non-stub `main`. Mirrors the boot sequence used by every
//! other service in the workspace (config → pool → migrate → serve).
//! Exposes the column-profiling / lint / rules surface plus the new
//! `dataset_health` aggregate consumed by the U4 QualityDashboard.

use std::net::SocketAddr;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use dataset_quality_service::{AppState, build_router, config::AppConfig};
use sqlx::postgres::PgPoolOptions;
use storage_abstraction::StorageBackend;
use storage_abstraction::local::LocalStorage;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("dataset_quality_service=info,tower_http=info")
            }),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    // Storage backend. The lint surface (and the upcoming row-count
    // sampler) need byte access; default to LocalStorage rooted at
    // `local_storage_root` and switch to S3 when configured.
    let storage: Arc<dyn StorageBackend> = match app_config.storage_backend.as_str() {
        "s3" => {
            let bucket = app_config.storage_bucket.clone();
            let region = app_config
                .s3_region
                .clone()
                .unwrap_or_else(|| "us-east-1".into());
            let access_key = app_config.s3_access_key.clone().unwrap_or_default();
            let secret_key = app_config.s3_secret_key.clone().unwrap_or_default();
            let endpoint = app_config.s3_endpoint.clone();
            let backend = storage_abstraction::s3::S3Storage::new(
                &bucket,
                &region,
                endpoint.as_deref(),
                &access_key,
                &secret_key,
            )?;
            Arc::new(backend)
        }
        _ => {
            let root = app_config
                .local_storage_root
                .clone()
                .unwrap_or_else(|| "/var/lib/openfoundry/quality".into());
            std::fs::create_dir_all(&root).ok();
            Arc::new(LocalStorage::new(&root)?)
        }
    };

    let jwt_config = Arc::new(JwtConfig::new(&app_config.jwt_secret).with_env_defaults());
    let state = AppState {
        db,
        storage,
        jwt_config,
    };
    let app = build_router(state);

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting dataset-quality-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
