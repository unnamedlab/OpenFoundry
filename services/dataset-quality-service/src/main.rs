mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, extract::FromRef, middleware,
    routing::{delete, get, patch, post},
};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;
use storage_abstraction::StorageBackend;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub storage: std::sync::Arc<dyn StorageBackend>,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("dataset-quality-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let storage = build_storage_backend(&cfg);

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        storage,
    };

    let public = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("dataset-quality-service")) }),
        )
        .route(
            "/internal/datasets/{id}/quality/refresh",
            post(handlers::quality::refresh_dataset_quality_internal),
        );

    let protected = Router::new()
        .route(
            "/api/v1/datasets/{id}/lint",
            get(handlers::lint::get_dataset_lint),
        )
        .route(
            "/api/v1/datasets/{id}/quality",
            get(handlers::quality::get_dataset_quality),
        )
        .route(
            "/api/v1/datasets/{id}/quality/profile",
            post(handlers::quality::refresh_dataset_quality),
        )
        .route(
            "/api/v1/datasets/{id}/quality/rules",
            post(handlers::quality::create_quality_rule),
        )
        .route(
            "/api/v1/datasets/{id}/quality/rules/{rule_id}",
            patch(handlers::quality::update_quality_rule),
        )
        .route(
            "/api/v1/datasets/{id}/quality/rules/{rule_id}",
            delete(handlers::quality::delete_quality_rule),
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
    tracing::info!("starting dataset-quality-service on {addr}");

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
