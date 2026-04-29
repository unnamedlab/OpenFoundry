//! `sql-warehousing-service` entrypoint.
//!
//! This binary exposes an [Apache Arrow Flight SQL](https://arrow.apache.org/docs/format/FlightSql.html)
//! server backed by DataFusion (via `libs/query-engine`) on the port declared
//! in `docs/architecture/services-and-ports.md`, and a tiny `axum` side router
//! exposing `/healthz` on a separate port for the platform health probes.

use std::net::SocketAddr;

use arrow_flight::flight_service_server::FlightServiceServer;
use axum::{Router, http::StatusCode, routing::get};
use sql_warehousing_service::config::AppConfig;
use sql_warehousing_service::flight_sql::FlightSqlServiceImpl;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("sql_warehousing_service=info,tonic=info")),
        )
        .init();

    let config = AppConfig::from_env()?;

    let flight_addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    let healthz_addr: SocketAddr = format!("{}:{}", config.host, config.healthz_port).parse()?;

    tracing::info!(%flight_addr, "starting Arrow Flight SQL server");
    tracing::info!(%healthz_addr, "starting healthz side router");

    let flight_service = FlightSqlServiceImpl::new();
    let flight_server = tonic::transport::Server::builder()
        .add_service(FlightServiceServer::new(flight_service))
        .serve(flight_addr);

    let healthz_app = Router::new()
        .route("/healthz", get(healthz))
        .route("/health", get(healthz));
    let healthz_listener = tokio::net::TcpListener::bind(healthz_addr).await?;
    let healthz_server = axum::serve(healthz_listener, healthz_app);

    tokio::select! {
        result = flight_server => {
            result?;
        }
        result = healthz_server => {
            result?;
        }
    }

    Ok(())
}

async fn healthz() -> (StatusCode, &'static str) {
    (StatusCode::OK, "ok")
}
