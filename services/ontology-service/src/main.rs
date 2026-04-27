#![allow(dead_code)]

mod config;

use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

#[tokio::main]
async fn main() {
    observability::init_tracing("ontology-service");

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

    let app = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("ontology-service")) }),
    );

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting ontology-service shell on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
