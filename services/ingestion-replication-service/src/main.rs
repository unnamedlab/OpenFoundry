//! `ingestion-replication-service` binary entry point.
//!
//! Wires the gRPC `IngestionControlPlane` server, the Postgres pool and the
//! Kubernetes client together and starts the reconcile loop. See the crate
//! docs in [`lib.rs`] for the architecture overview.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use ingestion_replication_service::app_config::AppConfig;
use ingestion_replication_service::grpc_service::{ControlPlaneService, ControlPlaneState};
use ingestion_replication_service::proto::ingestion_control_plane_server::IngestionControlPlaneServer;
use ingestion_replication_service::reconcile;
use kube::Client;
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("ingestion_replication_service=info,tonic=info,kube=info")
        }))
        .init();

    let config = AppConfig::from_env()?;
    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;

    let pool = PgPoolOptions::new()
        .max_connections(8)
        .connect(&config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&pool).await?;

    let kube_client = Client::try_default().await?;

    let state = ControlPlaneState {
        db: pool.clone(),
        kube: kube_client.clone(),
        default_namespace: Arc::from(config.default_namespace.as_str()),
    };

    // Background reconcile loop.
    let reconcile_pool = pool.clone();
    let reconcile_client = kube_client.clone();
    let period = Duration::from_secs(config.reconcile_period_secs);
    tokio::spawn(async move {
        reconcile::run(reconcile_pool, reconcile_client, period).await;
    });

    tracing::info!(%addr, "starting IngestionControlPlane gRPC server");
    tonic::transport::Server::builder()
        .add_service(IngestionControlPlaneServer::new(ControlPlaneService::new(
            state,
        )))
        .serve(addr)
        .await?;
    Ok(())
}
