//! `ontology-timeseries-analytics-service` binary entry point — substrate-only.
//!
//! ## Status: shell binary, migrated handlers not mounted
//!
//! Per `docs/architecture/legacy-migrations/ontology-timeseries-analytics-service/README.md`
//! saved dashboards/queries are declarative and are represented behind
//! `DefinitionStore`; the **runtime** time-series data lives in
//! `time-series-data-service` (P29) and is read through that service.
//! The local handler module is kept gate-clean and covered by unit tests,
//! but routes are not mounted until the exploratory consolidation lands.
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
#[allow(dead_code)]
mod handlers;
#[allow(dead_code)]
mod models;

use std::{net::SocketAddr, sync::Arc};

use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};
use storage_abstraction::repositories::{DefinitionStore, TenantId};

use crate::config::AppConfig;

#[allow(dead_code)]
#[derive(Clone)]
pub(crate) struct AppState {
    pub definitions: Arc<dyn DefinitionStore>,
    pub tenant: TenantId,
}

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
