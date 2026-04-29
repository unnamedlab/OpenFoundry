mod config;
mod domain;
mod handlers;
mod models;

use std::{net::SocketAddr, sync::Arc};

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, post, put},
};
use config::AppConfig;
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;
use vector_store::{BackendConfig, VectorBackendRouter, build_backend};

const SERVICE_NAME: &str = "retrieval-context-service";

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub http_client: reqwest::Client,
    pub jwt_config: JwtConfig,
    pub checkpoints_purpose_service_url: String,
    pub vector_router: Arc<VectorBackendRouter>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new(format!("{SERVICE_NAME}=info,tower_http=info"))
        }))
        .init();

    let config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&config.database_url)
        .await?;

    let default_backend_config = BackendConfig {
        kind: config.tenant.vector_backend,
        database_url: Some(config.database_url.clone()),
        vespa_url: config.vespa_url.clone(),
        dim: 768,
    };
    let default_backend = Arc::from(build_backend(&default_backend_config).await?);

    let mut router = VectorBackendRouter::new(default_backend);
    for (tenant_id, override_cfg) in &config.tenant.overrides {
        if let Some(kind) = override_cfg.vector_backend {
            let cfg = BackendConfig {
                kind,
                database_url: Some(config.database_url.clone()),
                vespa_url: config.vespa_url.clone(),
                dim: 768,
            };
            let backend = Arc::from(build_backend(&cfg).await?);
            router = router.with_override(tenant_id.clone(), backend);
        }
    }

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();
    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        jwt_config: jwt_config.clone(),
        checkpoints_purpose_service_url: config.checkpoints_purpose_service_url.clone(),
        vector_router: Arc::new(router),
    };

    let protected = Router::new()
        .route(
            "/knowledge-bases",
            get(handlers::knowledge::list_knowledge_bases)
                .post(handlers::knowledge::create_knowledge_base),
        )
        .route(
            "/knowledge-bases/{knowledge_base_id}",
            put(handlers::knowledge::update_knowledge_base),
        )
        .route(
            "/knowledge-bases/{knowledge_base_id}/documents",
            get(handlers::knowledge::list_documents)
                .post(handlers::knowledge::create_document),
        )
        .route(
            "/knowledge-bases/{knowledge_base_id}/search",
            post(handlers::knowledge::search_knowledge_base),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1", protected)
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting {SERVICE_NAME}");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}