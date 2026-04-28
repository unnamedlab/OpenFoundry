mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    extract::FromRef,
    middleware,
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
    pub http_client: reqwest::Client,
    pub dataset_quality_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("data-asset-catalog-service");

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
            get(|| async { axum::Json(HealthStatus::ok("data-asset-catalog-service")) }),
        )
        .route(
            "/internal/datasets/{id}/metadata",
            get(handlers::internal::get_dataset_metadata),
        );

    let protected = Router::new()
        .route("/api/v1/datasets", post(handlers::crud::create_dataset))
        .route("/api/v1/datasets", get(handlers::crud::list_datasets))
        .route(
            "/api/v1/datasets/catalog/facets",
            get(handlers::catalog::get_catalog_facets),
        )
        .route("/api/v1/datasets/{id}", get(handlers::crud::get_dataset))
        .route(
            "/api/v1/datasets/{id}",
            patch(handlers::crud::update_dataset),
        )
        .route(
            "/api/v1/datasets/{id}",
            delete(handlers::crud::delete_dataset),
        )
        .route(
            "/api/v1/datasets/{id}/upload",
            post(handlers::upload::upload_data),
        )
        .route(
            "/api/v1/datasets/{id}/preview",
            get(handlers::preview::preview_data),
        )
        .route(
            "/api/v1/datasets/{id}/schema",
            get(handlers::preview::get_schema),
        )
        .route(
            "/api/v1/datasets/{id}/files",
            get(handlers::export::list_files),
        )
        .route(
            "/api/v1/datasets/{id}/filesystem",
            get(handlers::export::list_files),
        )
        .route(
            "/api/v2/filesystem/datasets/{id}",
            get(handlers::export::list_files),
        )
        .route(
            "/api/v1/datasets/{id}/views",
            get(handlers::views::list_views),
        )
        .route(
            "/api/v1/datasets/{id}/views",
            post(handlers::views::create_view),
        )
        .route(
            "/api/v1/datasets/{id}/views/{view_id}",
            get(handlers::views::get_view),
        )
        .route(
            "/api/v1/datasets/{id}/views/{view_id}/preview",
            get(handlers::views::preview_view),
        )
        .route(
            "/api/v1/datasets/{id}/views/{view_id}/refresh",
            post(handlers::views::refresh_view),
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
    tracing::info!("starting data-asset-catalog-service on {addr}");

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
