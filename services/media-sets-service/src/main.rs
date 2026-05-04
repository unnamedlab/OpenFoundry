//! `media-sets-service` binary entry point.
//!
//! Boots the REST router on `config.port` and the Tonic gRPC server on
//! `config.grpc_port` (default `port + 1`) inside the same `tokio::main`
//! runtime, sharing one [`AppState`] backed by a [`DualPool`] (writer +
//! optional reader replica) and a [`BackendMediaStorage`] over an
//! S3-compatible object store.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use auth_middleware::jwt::JwtConfig;
use authz_cedar::{AuthzEngine, PolicyStore, audit::TracingAuditSink};
use db_pool::DualPool;
use media_sets_service::domain::cedar::default_policy_records;
use media_sets_service::domain::retention;
use media_sets_service::{
    AppState, BackendMediaStorage, MediaStorage, build_router, config::AppConfig,
    grpc::build_server,
};
use storage_abstraction::backend::StorageBackend;
use storage_abstraction::local::LocalStorage;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("media_sets_service=info,tower_http=info,tonic=info")
        }))
        .init();

    let cfg = AppConfig::from_env()?;
    let http_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    let grpc_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.resolved_grpc_port()).parse()?;

    let pool = DualPool::connect(&cfg.database_url, None, Default::default()).await?;
    sqlx::migrate!("./migrations").run(pool.writer()).await?;

    // Local backend is the default for dev / single-node; the production
    // path swaps in the S3 backend through configuration once it is
    // wired in `libs/storage-abstraction`.
    std::fs::create_dir_all(&cfg.storage_root).ok();
    let backend: Arc<dyn StorageBackend> = Arc::new(LocalStorage::new(&cfg.storage_root)?);
    let storage: Arc<dyn MediaStorage> = Arc::new(BackendMediaStorage::new(
        backend,
        cfg.storage_bucket.clone(),
        cfg.storage_endpoint.clone(),
    ));

    // Cedar engine seeded with the bundled media-set defaults; the
    // production hot-reload path will overlay the active policy set
    // from `pg-policy.cedar_policies` (ADR-0027) once the
    // `policy-decision-service` writer goes live.
    let policy_store = PolicyStore::with_policies(&default_policy_records()).await?;
    let engine = Arc::new(AuthzEngine::new(policy_store, Arc::new(TracingAuditSink)));

    let state = AppState {
        db: pool.clone(),
        jwt_config: Arc::new(JwtConfig::new(&cfg.jwt_secret)),
        storage: storage.clone(),
        presign_ttl_seconds: cfg.presign_ttl_seconds,
        http: reqwest::Client::new(),
        connector_service_url: std::env::var("CONNECTOR_SERVICE_URL").ok(),
        engine,
        presign_secret: Arc::new(cfg.jwt_secret.as_bytes().to_vec()),
    };

    let router = build_router(state.clone());
    let grpc = build_server(state.clone());

    let http_listener = tokio::net::TcpListener::bind(http_addr).await?;

    // Background retention reaper. Period is configurable via
    // RETENTION_REAPER_SECS (default 300s = 5 min).
    let reaper_secs = std::env::var("RETENTION_REAPER_SECS")
        .ok()
        .and_then(|s| s.parse::<u64>().ok())
        .unwrap_or(300);
    let _reaper = retention::spawn_reaper(
        pool.writer().clone(),
        storage.clone(),
        Duration::from_secs(reaper_secs),
    );
    tracing::info!(reaper_secs, "retention reaper running");

    tracing::info!(%http_addr, %grpc_addr, "media-sets-service listening (REST + gRPC)");

    let http_task = tokio::spawn(async move {
        axum::serve(http_listener, router.into_make_service())
            .await
            .map_err(|e| Box::<dyn std::error::Error + Send + Sync>::from(e))
    });
    let grpc_task = tokio::spawn(async move {
        tonic::transport::Server::builder()
            .add_service(grpc)
            .serve(grpc_addr)
            .await
            .map_err(|e| Box::<dyn std::error::Error + Send + Sync>::from(e))
    });

    tokio::select! {
        res = http_task => match res? {
            Ok(()) => Ok(()),
            Err(e) => Err(Box::<dyn std::error::Error>::from(e.to_string())),
        },
        res = grpc_task => match res? {
            Ok(()) => Ok(()),
            Err(e) => Err(Box::<dyn std::error::Error>::from(e.to_string())),
        },
    }
}
