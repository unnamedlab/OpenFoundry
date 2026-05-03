//! `monitoring-rules-service` Axum binary.
//!
//! Mounts the monitor CRUD endpoints and the evaluator scheduler.
//! P4 wires up the streaming-monitor surface; the legacy
//! `monitoring_rules`/`monitoring_subscribers` endpoints from the
//! action-monitoring contract live next to the new ones under
//! `/api/v1/monitoring/*` so the existing UI clients keep working.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use auth_middleware::jwt::JwtConfig;
use auth_middleware::layer::auth_layer;
use axum::{
    Router,
    routing::{get, patch, post},
};
use monitoring_rules_service::evaluator::{
    HttpMetricsSource, HttpNotifier, MetricsSource, Notifier, spawn_scheduler,
};
use monitoring_rules_service::{AppState, config::AppConfig, handlers, streaming_handlers};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("monitoring_rules_service=info,tower_http=info,audit=info")
        }))
        .init();

    let cfg = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();

    let state = AppState { db: db.clone() };

    // ---- evaluator scheduler ------------------------------------------------
    let metrics_url = std::env::var("STREAMING_BASE_URL")
        .unwrap_or_else(|_| "http://event-streaming-service:50079".to_string());
    let notifier_url = std::env::var("NOTIFICATION_BASE_URL")
        .unwrap_or_else(|_| "http://notification-alerting-service:50083".to_string());
    let http = reqwest::Client::builder()
        .user_agent("monitoring-rules-service/0.0.1")
        .build()?;
    let metrics: Arc<dyn MetricsSource> = Arc::new(HttpMetricsSource {
        base_url: metrics_url,
        http: http.clone(),
    });
    let notifier: Arc<dyn Notifier> = Arc::new(HttpNotifier {
        base_url: notifier_url,
        http,
        bearer: std::env::var("MONITOR_INTERNAL_BEARER").ok(),
    });
    let interval = Duration::from_secs(
        std::env::var("MONITOR_EVAL_INTERVAL_SECONDS")
            .ok()
            .and_then(|raw| raw.parse().ok())
            .unwrap_or(60),
    );
    let _scheduler = spawn_scheduler(db.clone(), metrics, notifier, interval);
    tracing::info!(
        interval_secs = interval.as_secs(),
        "monitor scheduler spawned"
    );

    // ---- HTTP router --------------------------------------------------------
    let public = Router::new()
        // legacy action-monitoring contract
        .route(
            "/items",
            get(handlers::list_items).post(handlers::create_item),
        )
        .route("/items/{id}", get(handlers::get_item))
        .route(
            "/items/{id}/secondary",
            get(handlers::list_secondary).post(handlers::create_secondary),
        )
        .route("/rules", post(handlers::create_action_rule))
        // streaming monitors (P4)
        .route(
            "/monitoring-views",
            get(streaming_handlers::list_views).post(streaming_handlers::create_view),
        )
        .route("/monitoring-views/{id}", get(streaming_handlers::get_view))
        .route(
            "/monitoring-views/{id}/rules",
            get(streaming_handlers::list_rules_for_view),
        )
        .route(
            "/monitor-rules",
            get(streaming_handlers::list_rules).post(streaming_handlers::create_rule),
        )
        .route(
            "/monitor-rules/{id}",
            patch(streaming_handlers::patch_rule).delete(streaming_handlers::delete_rule),
        )
        .route(
            "/monitor-rules/{id}/evaluations",
            get(streaming_handlers::list_evaluations),
        )
        .with_state(state)
        .layer(axum::middleware::from_fn_with_state(jwt_config, auth_layer));

    let app = Router::new()
        .route("/health", get(|| async { "ok" }))
        .nest("/api/v1/monitoring", public);

    let addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    tracing::info!(%addr, "starting monitoring-rules-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}
