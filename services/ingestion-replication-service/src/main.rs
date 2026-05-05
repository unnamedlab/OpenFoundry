//! `ingestion-replication-service` binary entry point.
//!
//! Wires the gRPC `IngestionControlPlane` server, the Postgres pool and the
//! Kubernetes client together and starts the reconcile loop. See the crate
//! docs in [`lib.rs`] for the architecture overview.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use ingestion_replication_service::app_config::AppConfig;
use ingestion_replication_service::cdc_metadata;
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

    if let Some(cdc_metadata_database_url) = &config.cdc_metadata_database_url {
        let cdc_metadata_addr: SocketAddr =
            format!("{}:{}", config.cdc_metadata_host, config.cdc_metadata_port).parse()?;
        let cdc_metadata_pool = PgPoolOptions::new()
            .max_connections(8)
            .connect(cdc_metadata_database_url)
            .await?;
        sqlx::migrate!("./migrations/cdc_metadata")
            .run(&cdc_metadata_pool)
            .await?;
        let cdc_metadata_app = cdc_metadata::routes().with_state(cdc_metadata::AppState {
            db: cdc_metadata_pool,
        });
        tokio::spawn(async move {
            let listener = match tokio::net::TcpListener::bind(cdc_metadata_addr).await {
                Ok(listener) => listener,
                Err(error) => {
                    tracing::error!(%cdc_metadata_addr, %error, "failed to bind CDC metadata HTTP server");
                    return;
                }
            };
            tracing::info!(%cdc_metadata_addr, "starting CDC metadata HTTP server");
            if let Err(error) = axum::serve(listener, cdc_metadata_app).await {
                tracing::error!(%error, "CDC metadata HTTP server failed");
            }
        });
    }

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
