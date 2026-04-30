//! `sql-bi-gateway-service` entrypoint.
//!
//! Boots two concurrent surfaces:
//!
//! 1. **Flight SQL gRPC** on `port` (default `50133`) — the primary edge
//!    surface used by Tableau, Superset and any JDBC/ODBC client speaking
//!    the Apache Arrow Flight SQL protocol. This replaces the Trino
//!    coordinator that previously lived under `infra/k8s/trino/` (see
//!    [ADR-0014]).
//!
//! 2. **HTTP side router** on `healthz_port` (default `50134`) — exposes
//!    `/healthz` for Kubernetes probes and the saved-queries CRUD used
//!    by the BI dashboards.
//!
//! Backend endpoints (`*_FLIGHT_SQL_URL` env vars) are optional: a
//! gateway with no remote endpoints configured runs as a pure local
//! DataFusion node and rejects statements that target unconfigured
//! backends with `failed_precondition` and a clear message.
//!
//! [ADR-0014]: ../../docs/architecture/adr/ADR-0014-retire-trino-flight-sql-only.md

use std::net::SocketAddr;
use std::sync::Arc;

use arrow_flight::flight_service_server::FlightServiceServer;
use sql_bi_gateway_service::auth::Authenticator;
use sql_bi_gateway_service::config::AppConfig;
use sql_bi_gateway_service::flight_sql::FlightSqlServiceImpl;
use sql_bi_gateway_service::http::build_router;
use sql_bi_gateway_service::routing::BackendRouter;
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("sql_bi_gateway_service=info,tonic=info")),
        )
        .init();

    let config = AppConfig::from_env()?;

    let flight_addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    let healthz_addr: SocketAddr = format!("{}:{}", config.host, config.healthz_port).parse()?;

    tracing::info!(%flight_addr, "starting Arrow Flight SQL server (sql-bi-gateway-service)");
    tracing::info!(%healthz_addr, "starting HTTP side router (healthz + saved queries)");

    let router = BackendRouter::from_config(&config);
    let auth = Authenticator::new(&config.jwt_secret, config.allow_anonymous);
    let flight_service = FlightSqlServiceImpl::new(router, auth);
    let flight_server = tonic::transport::Server::builder()
        .add_service(FlightServiceServer::new(flight_service))
        .serve(flight_addr);

    // Best-effort connection to the saved-queries Postgres cluster; if it
    // is not reachable the side router still serves `/healthz` so the
    // Flight SQL surface stays available during BI database outages.
    let db = match PgPoolOptions::new()
        .max_connections(8)
        .connect(&config.database_url)
        .await
    {
        Ok(pool) => Some(Arc::new(pool)),
        Err(error) => {
            tracing::warn!(
                error = %error,
                "saved-queries Postgres unreachable; HTTP router will only expose /healthz"
            );
            None
        }
    };

    let healthz_app = build_router(db);
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
