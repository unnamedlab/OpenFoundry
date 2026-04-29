//! `ingestion-replication-service` binary entry point.
//!
//! Wires the gRPC server, the Postgres pool and the Kubernetes client
//! together and starts the reconcile loop. See [`README.md`] for the full
//! design.
//!
//! [`README.md`]: ../README.md

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    http::StatusCode,
    routing::{get, post},
};
use ingestion_replication_service::{
    AppState,
    config::AppConfig,
    domain::scheduler,
    grpc::IngestJobServiceImpl,
    open_foundry::data_integration::ingest_job_service_server::IngestJobServiceServer,
};
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
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("ingestion_replication_service=info,tonic=info,tower_http=info")
                EnvFilter::new("ingestion_replication_service=info,tonic=info,kube=info")
            }),
        )
        .init();

    let config = AppConfig::from_env()?;

    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();

    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        jwt_config: jwt_config.clone(),
        dataset_service_url: config.dataset_service_url.clone(),
        allow_private_network_egress: config.allow_private_network_egress,
        allowed_egress_hosts: config.allowed_egress_hosts.clone(),
        agent_stale_after: Duration::from_secs(config.agent_stale_after_secs),
    };

    // ── HTTP server (REST API) ────────────────────────────────────────────────

    let protected = Router::new()
        .route(
            "/connections/:id/sync",
            post(ingestion_replication_service::handlers::sync_ops::sync_connection),
        )
        .route(
            "/connections/:id/sync-jobs",
            get(ingestion_replication_service::handlers::sync_ops::list_sync_jobs),
        )
        .route(
            "/connector-agents",
            get(ingestion_replication_service::handlers::agents::list_agents)
                .post(ingestion_replication_service::handlers::agents::register_agent),
        )
        .route(
            "/connector-agents/:id/heartbeat",
            post(ingestion_replication_service::handlers::agents::heartbeat_agent),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let http_app = Router::new()
        .nest("/api/v1", protected)
        .route(
            "/internal/sync-jobs",
            post(ingestion_replication_service::handlers::sync_ops::queue_internal_sync_job),
        )
        .route("/healthz", get(healthz))
        .route("/health", get(healthz))
        .with_state(state.clone());

    let http_addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%http_addr, "starting HTTP server");
    let http_listener = tokio::net::TcpListener::bind(http_addr).await?;

    // ── Scheduler ─────────────────────────────────────────────────────────────

    let scheduler_state = state.clone();
    let poll = Duration::from_secs(config.sync_poll_interval_secs);
    tokio::spawn(async move {
        scheduler::run_scheduler(scheduler_state, poll).await;
    });

    // ── gRPC server (IngestJobService) ────────────────────────────────────────

    let grpc_addr: SocketAddr = format!("{}:{}", config.host, config.grpc_port).parse()?;
    tracing::info!(%grpc_addr, "starting gRPC IngestJobService server");

    let svc = IngestJobServiceImpl::new(Arc::new(state));
    let grpc_server = tonic::transport::Server::builder()
        .add_service(IngestJobServiceServer::new(svc))
        .serve(grpc_addr);

    // ── Run all three concurrently ────────────────────────────────────────────

    tokio::select! {
        result = axum::serve(http_listener, http_app) => {
            result?;
        }
        result = grpc_server => {
            result?;
        }
    }

    Ok(())
}

async fn healthz() -> (StatusCode, &'static str) {
    (StatusCode::OK, "ok")
}
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
