#![allow(dead_code)]

mod config;

use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};

#[tokio::main]
async fn main() {
    observability::init_tracing("pipeline-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let app = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("pipeline-service")) }),
    );

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting pipeline-service on {addr}");
    tracing::info!(
        "pipeline-service now acts as compatibility shell; authoring and compilation moved to pipeline-authoring-service"
    );

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
