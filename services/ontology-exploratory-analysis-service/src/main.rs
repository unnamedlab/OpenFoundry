//! `ontology-exploratory-analysis-service` binary entry point — substrate-only.
//!
//! ## Status: shell binary, no domain handlers wired
//!
//! Per `docs/architecture/legacy-migrations/ontology-exploratory-analysis-service/README.md`
//! the per-handler migration to `Arc<dyn ObjectStore>` + the
//! Cassandra-backed writeback queue is a follow-up
//! (mirrors the S1.4.b / S1.5.f deferrals for funnel and functions).
//! No handlers for this bounded context exist in
//! `libs/ontology-kernel/src/handlers/` yet, so this binary cannot
//! "wire kernel handlers" the way `ontology-funnel-service` /
//! `ontology-functions-service` / `ontology-security-service` do.
//!
//! ## Consolidation status
//!
//! This crate is the **target** of three pending merges per
//! `docs/architecture/service-consolidation-map.md`:
//! `ontology-timeseries-analytics-service`,
//! `time-series-data-service`, `geospatial-intelligence-service`,
//! `scenario-simulation-service` → `ontology-exploratory-analysis-service`
//! (all `merge → exploratory`, pending). Until those merges land and
//! the kernel grows `handlers::exploratory`, this binary just exposes
//! `/health` and `/readiness` so `cargo run -p ontology-exploratory-analysis-service`
//! still produces a live process for orchestrators that probe it.
//!
//! Schema split (S1.7): `exploratory_views` / `exploratory_maps` go to
//! `pg-schemas.ontology_schema`; `writeback_proposals` becomes a
//! Cassandra `actions_log` queue routed through
//! `ontology-actions-service`'s writeback helper.

mod config;
#[allow(dead_code)] // Models used by the future handler migration.
mod models;

use std::net::SocketAddr;

use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};

use crate::config::AppConfig;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    observability::init_tracing("ontology-exploratory-analysis-service");

    let cfg = AppConfig::from_env()?;

    let app = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("ontology-exploratory-analysis-service")) }),
        )
        .route(
            "/readiness",
            get(|| async { axum::Json(HealthStatus::ok("ontology-exploratory-analysis-service")) }),
        );

    let addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    tracing::info!(
        %addr,
        "starting ontology-exploratory-analysis-service (substrate-only — no domain handlers wired)"
    );

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;
    Ok(())
}

async fn shutdown_signal() {
    let ctrl_c = async {
        let _ = tokio::signal::ctrl_c().await;
    };
    #[cfg(unix)]
    let terminate = async {
        if let Ok(mut sig) =
            tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        {
            sig.recv().await;
        }
    };
    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();
    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
    tracing::info!("graceful shutdown signal received");
}
