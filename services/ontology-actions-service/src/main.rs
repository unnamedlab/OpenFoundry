//! `ontology-actions-service` binary entry point.
//!
//! Owns the writeback surface of the Action types feature
//! (`/api/v1/ontology/actions/*` and the per-property inline-edit route).
//! All business logic lives in `libs/ontology-kernel::handlers::actions`;
//! this binary just wires configuration, the Postgres pool, the JWT layer
//! and the Axum router built by [`ontology_actions_service::build_router`].

use std::net::SocketAddr;
use std::sync::Arc;

use axum::{
    Router,
    extract::Extension,
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::get,
};
use ontology_actions_service::{build_router, config::AppConfig, jwt_config_from_secret};
use ontology_kernel::AppState;
use sqlx::postgres::PgPoolOptions;
use tower_http::trace::TraceLayer;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("ontology_actions_service=info,ontology_kernel=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;

    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        jwt_config: jwt_config_from_secret(&app_config.jwt_secret),
        audit_service_url: app_config.audit_service_url.clone(),
        dataset_service_url: app_config.dataset_service_url.clone(),
        ontology_service_url: app_config.ontology_service_url.clone(),
        pipeline_service_url: app_config.pipeline_service_url.clone(),
        ai_service_url: app_config.ai_service_url.clone(),
        notification_service_url: app_config.notification_service_url.clone(),
        search_embedding_provider: app_config.search_embedding_provider.clone(),
        node_runtime_command: app_config.node_runtime_command.clone(),
        connector_management_service_url: app_config
            .connector_management_service_url
            .clone(),
    };

    let registry = Arc::new(prometheus::Registry::new());
    ontology_kernel::metrics::register_action_metrics(&registry);

    let app = Router::new()
        .merge(build_router(state))
        .route("/health", get(|| async { "ok" }))
        .route("/metrics", get(render_metrics))
        .layer(Extension(registry))
        .layer(TraceLayer::new_for_http());

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting ontology-actions-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

/// Prometheus exposition endpoint. The registry is shared via an Axum
/// `Extension` so the route remains state-agnostic and can sit alongside
/// the kernel's `Router<AppState>` returned by [`build_router`]. Counters
/// will be populated when TASK F (Action metrics) lands.
async fn render_metrics(Extension(registry): Extension<Arc<prometheus::Registry>>) -> Response {
    use prometheus::Encoder;
    let encoder = prometheus::TextEncoder::new();
    let mut buffer = Vec::new();
    if let Err(error) = encoder.encode(&registry.gather(), &mut buffer) {
        tracing::error!(?error, "failed to encode prometheus metrics");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    (
        StatusCode::OK,
        [(axum::http::header::CONTENT_TYPE, encoder.format_type())],
        buffer,
    )
        .into_response()
}
