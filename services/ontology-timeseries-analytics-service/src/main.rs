//! `ontology-timeseries-analytics-service` binary entry point — substrate-only.
//!
//! ## Status: shell binary, no domain handlers wired
//!
//! Per `docs/architecture/legacy-migrations/ontology-timeseries-analytics-service/README.md`
//! both legacy tables (`ontology_timeseries_dashboards`,
//! `ontology_timeseries_queries`) are declarative and move whole to
//! `pg-schemas.ontology_schema`; the **runtime** time-series data
//! lives in `time-series-data-service` (P29) and is read through that
//! service. There is therefore no Cassandra component for this
//! bounded context and no kernel handlers in
//! `libs/ontology-kernel/src/handlers/` to mount here.
//!
//! ## Consolidation status
//!
//! Per `docs/architecture/service-consolidation-map.md` this crate is
//! `merge → ontology-exploratory-analysis-service` (pending). Once the
//! merge lands, dashboard/saved-query routes will be served by the
//! exploratory binary directly. Until then this binary exposes only
//! `/health` and `/readiness` so `cargo run -p ontology-timeseries-analytics-service`
//! still produces a live process for orchestrators that probe it.

mod config;
#[allow(dead_code)] // Models retained for the future merge into exploratory.
mod models;

use std::net::SocketAddr;

use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};

use crate::config::AppConfig;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    observability::init_tracing("ontology-timeseries-analytics-service");

    let cfg = AppConfig::from_env()?;

    let app = Router::new()
        .route(
            "/health",
            get(|| async { axum::Json(HealthStatus::ok("ontology-timeseries-analytics-service")) }),
        )
        .route(
            "/readiness",
            get(|| async { axum::Json(HealthStatus::ok("ontology-timeseries-analytics-service")) }),
        );

    let addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    tracing::info!(
        %addr,
        "starting ontology-timeseries-analytics-service (substrate-only — no domain handlers wired)"
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
