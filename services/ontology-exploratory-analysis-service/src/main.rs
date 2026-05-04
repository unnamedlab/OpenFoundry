//! `ontology-exploratory-analysis-service` binary entry point — substrate-only.
//!
//! ## Status: shell binary, migrated handlers not mounted
//!
//! Per `docs/architecture/legacy-migrations/ontology-exploratory-analysis-service/README.md`
//! the local handlers now route saved views/maps through `DefinitionStore`
//! and writeback proposals through `ActionLogStore`. The binary still only
//! mounts substrate probes until the service-consolidation merge promotes
//! these routes to the public surface.
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
//! Schema split (S1.7): `exploratory_views` / `exploratory_maps` are
//! declarative `DefinitionStore` rows; `writeback_proposals` is represented
//! as a Cassandra-compatible `actions_log` append via `ActionLogStore`.

mod config;
#[allow(dead_code)]
mod handlers;
#[allow(dead_code)]
mod models;

use std::{net::SocketAddr, sync::Arc};

use axum::{Router, routing::get};
use core_models::{health::HealthStatus, observability};
use storage_abstraction::repositories::{ActionLogStore, DefinitionStore, TenantId};

use crate::config::AppConfig;

#[allow(dead_code)]
#[derive(Clone)]
pub(crate) struct AppState {
    pub definitions: Arc<dyn DefinitionStore>,
    pub actions: Arc<dyn ActionLogStore>,
    pub tenant: TenantId,
    pub subject: String,
}

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
