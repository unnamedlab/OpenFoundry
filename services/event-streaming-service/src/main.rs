//! `event-streaming-service` entrypoint.
//!
//! Boots three concurrent listeners:
//!
//! * the public REST control plane on `rest_port` (default `50121`,
//!   proxied by the edge gateway under `/api/v1/streaming/*`),
//! * the gRPC routing facade (`Publish` / `Subscribe`) on `grpc_port`
//!   (default `50221`),
//! * an admin HTTP side router exposing `/healthz` and `/metrics` on
//!   `admin_port` (default `50222`).
//!
//! At startup we connect to Postgres, run `sqlx::migrate!`, build the
//! Iceberg/legacy dataset writer, optionally load the routing table from
//! `topic-routes.yaml` and instantiate the backend registry. The same
//! [`AppState`] is shared between the REST handlers and the streaming
//! engine.

use std::net::SocketAddr;
use std::path::Path;
use std::sync::Arc;

use axum::{
    Router,
    extract::State,
    http::StatusCode,
    middleware,
    response::IntoResponse,
    routing::{get, post, put},
};
use sqlx::postgres::PgPoolOptions;
use storage_abstraction::{StorageBackend, local::LocalStorage};
use tracing_subscriber::EnvFilter;

use auth_middleware::{jwt::JwtConfig, layer::auth_layer};
use event_streaming_service::AppState;
use event_streaming_service::app_config::AppConfig;
use event_streaming_service::backends::{BackendRegistry, KafkaUnavailableBackend, NatsBackend};
#[cfg(feature = "kafka-rdkafka")]
use event_streaming_service::backends::RdKafkaBackend;
use event_streaming_service::domain::archiver::ArchiverSupervisor;
use event_streaming_service::domain::checkpoints::CheckpointSupervisor;
use event_streaming_service::domain::engine::state_store::{
    InMemoryStateBackend, SharedStateBackend,
};
use event_streaming_service::domain::hot_buffer::{HotBuffer, NatsHotBuffer, NoopHotBuffer};
#[cfg(feature = "kafka-rdkafka")]
use event_streaming_service::domain::hot_buffer::KafkaHotBuffer;
use event_streaming_service::grpc::EventRouterService;
use event_streaming_service::handlers::{
    branches as branch_handlers,
    checkpoints as checkpoint_handlers,
    flink as flink_handlers,
    schemas as schema_handlers,
    streams,
    topologies,
};
use event_streaming_service::metrics::Metrics;
use event_streaming_service::router::{BackendId, RouteTable, RouterConfig};
use event_streaming_service::storage::{
    IcebergSettings, WriterBackendKind, WriterSettings, build_dataset_writer,
};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("event_streaming_service=info,tonic=info,audit=info")
        }))
        .init();

    let cfg = AppConfig::from_env()?;

    // ---- Postgres + migrations -------------------------------------------------
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;
    tracing::info!("postgres connected and migrations applied");

    // ---- Auth ------------------------------------------------------------------
    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();

    // ---- Dataset writer (Iceberg / legacy) -------------------------------------
    let storage: Arc<dyn StorageBackend> = Arc::new(LocalStorage::new(&cfg.archive_dir)?);
    let writer_settings = WriterSettings {
        backend: WriterBackendKind::parse(
            std::env::var("EVENT_STREAMING_WRITER_BACKEND")
                .unwrap_or_else(|_| "legacy".into())
                .as_str(),
        ),
        iceberg: IcebergSettings {
            catalog_url: cfg.iceberg_catalog_url.clone(),
            namespace: cfg.iceberg_namespace.clone(),
        },
    };
    let dataset_writer = build_dataset_writer(storage, &writer_settings);

    // ---- Routing table + backend registry --------------------------------------
    let (table, backends) = build_routing_runtime(&cfg).await?;
    let metrics = Arc::new(Metrics::new());

    // ---- Hot buffer (Kafka if compiled in + bootstrap servers, else NATS) -----
    let hot_buffer = build_hot_buffer(&cfg).await;
    tracing::info!(backend = hot_buffer.id(), "hot buffer ready");

    // ---- Cold-tier archiver ----------------------------------------------------
    let archiver_http = reqwest::Client::builder()
        .user_agent("event-streaming-service/0.0.1 (archiver)")
        .build()?;
    let archiver = ArchiverSupervisor::spawn(
        db.clone(),
        Arc::clone(&dataset_writer),
        archiver_http,
        cfg.dataset_service_url.clone(),
    )
    .await?;
    tracing::info!("cold-tier archiver supervisor spawned");

    // ---- Operator state backend (Bloque C) ------------------------------------
    let state_backend: SharedStateBackend = Arc::new(InMemoryStateBackend::new());
    tracing::info!(backend = state_backend.id(), "state backend ready");

    // ---- Checkpoint supervisor (per-topology periodic snapshots) --------------
    let checkpoints = CheckpointSupervisor::spawn(
        db.clone(),
        Arc::clone(&state_backend),
    )
    .await?;
    tracing::info!("checkpoint supervisor spawned");

    // ---- Flink runtime config + metrics poller (Bloque D) -------------------
    let flink_config = Arc::new(
        event_streaming_service::runtime::flink::FlinkRuntimeConfig::from_env(),
    );
    tracing::info!(
        namespace = %flink_config.default_namespace,
        flink_version = %flink_config.flink_version,
        "flink runtime config loaded"
    );
    #[cfg(feature = "flink-runtime")]
    let flink_poller = event_streaming_service::runtime::flink::metrics_poller::MetricsPollerSupervisor::spawn(
        db.clone(),
        Arc::clone(&flink_config),
    )
    .await?;
    #[cfg(feature = "flink-runtime")]
    tracing::info!("flink metrics poller spawned");

    // ---- AppState --------------------------------------------------------------
    let app_state = AppState {
        db,
        jwt_config: jwt_config.clone(),
        http_client: reqwest::Client::builder()
            .user_agent("event-streaming-service/0.0.1")
            .build()?,
        backends: Arc::clone(&backends),
        table: Arc::clone(&table),
        metrics: Arc::clone(&metrics),
        dataset_writer,
        hot_buffer,
        state_backend: Arc::clone(&state_backend),
        connector_management_service_url: cfg.connector_management_service_url.clone(),
        dataset_service_url: cfg.dataset_service_url.clone(),
        archive_dir: cfg.archive_dir.clone(),
        flink_config: Arc::clone(&flink_config),
    };

    // ---- REST control plane ----------------------------------------------------
    let rest_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.rest_port).parse()?;
    let rest_app = build_rest_router(app_state, jwt_config);
    let rest_listener = tokio::net::TcpListener::bind(rest_addr).await?;
    let rest_server = axum::serve(rest_listener, rest_app);
    tracing::info!(%rest_addr, "starting REST control plane (/api/v1/streaming/*)");

    // ---- gRPC routing facade ---------------------------------------------------
    let grpc_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.grpc_port).parse()?;
    let svc = EventRouterService::new(Arc::clone(&table), Arc::clone(&backends), Arc::clone(&metrics));
    let grpc_server = tonic::transport::Server::builder()
        .add_service(svc.into_server())
        .serve(grpc_addr);
    tracing::info!(%grpc_addr, "starting EventRouter gRPC server");

    // ---- Admin side router (/healthz, /metrics) --------------------------------
    let admin_addr: SocketAddr = format!("{}:{}", cfg.host, cfg.admin_port).parse()?;
    let admin_app = Router::new()
        .route("/healthz", get(healthz))
        .route("/health", get(healthz))
        .route("/metrics", get(metrics_handler))
        .with_state(metrics);
    let admin_listener = tokio::net::TcpListener::bind(admin_addr).await?;
    let admin_server = axum::serve(admin_listener, admin_app);
    tracing::info!(%admin_addr, "starting admin side router (/healthz, /metrics)");
    let _ = BackendId::Kafka; // keep the symbol referenced for clarity

    tokio::select! {
        result = rest_server => {
            archiver.shutdown();
            checkpoints.shutdown();
            #[cfg(feature = "flink-runtime")] flink_poller.shutdown();
            result?
        },
        result = grpc_server => {
            archiver.shutdown();
            checkpoints.shutdown();
            #[cfg(feature = "flink-runtime")] flink_poller.shutdown();
            result?
        },
        result = admin_server => {
            archiver.shutdown();
            checkpoints.shutdown();
            #[cfg(feature = "flink-runtime")] flink_poller.shutdown();
            result?
        },
    }
    Ok(())
}

/// Compose the public REST router. Every `/api/v1/streaming/*` route runs
/// behind both `auth-middleware::auth_layer` (JWT verification) and
/// `audit-trail::audit_layer` (per-request audit event).
fn build_rest_router(state: AppState, jwt_config: JwtConfig) -> Router {
    let api = Router::new()
        // overview & catalogues
        .route("/overview", get(topologies::get_overview))
        .route("/connectors", get(topologies::list_connectors))
        .route("/live-tail", get(topologies::get_live_tail))
        // streams
        .route(
            "/streams",
            get(streams::list_streams).post(streams::create_stream),
        )
        .route("/streams/{id}", put(streams::update_stream))
        .route("/streams/{id}/push", post(streams::push_events))
        .route("/streams/{id}/read", get(streams::read_stream))
        .route("/streams/{id}/dead-letters", get(streams::list_dead_letters))
        .route(
            "/dead-letters/{dl_id}/replay",
            post(streams::replay_dead_letter),
        )
        // windows
        .route(
            "/windows",
            get(streams::list_windows).post(streams::create_window),
        )
        .route("/windows/{id}", put(streams::update_window))
        // topologies
        .route(
            "/topologies",
            get(topologies::list_topologies).post(topologies::create_topology),
        )
        .route("/topologies/{id}", put(topologies::update_topology))
        .route("/topologies/{id}/runtime", get(topologies::get_runtime))
        .route("/topologies/{id}/run", post(topologies::run_topology))
        .route("/topologies/{id}/replay", post(topologies::replay_topology))
        .route(
            "/topologies/{id}/checkpoints",
            get(checkpoint_handlers::list_checkpoints).post(checkpoint_handlers::trigger_checkpoint),
        )
        .route(
            "/topologies/{id}/reset",
            post(checkpoint_handlers::reset_topology),
        )
        .route(
            "/topologies/{id}/deploy",
            post(flink_handlers::deploy_topology),
        )
        .route(
            "/topologies/{id}/job-graph",
            get(flink_handlers::get_job_graph),
        )
        // branches (Bloque E1)
        .route(
            "/streams/{id}/branches",
            get(branch_handlers::list_branches).post(branch_handlers::create_branch),
        )
        .route(
            "/streams/{id}/branches/{name}",
            get(branch_handlers::get_branch).delete(branch_handlers::delete_branch),
        )
        .route(
            "/streams/{id}/branches/{name}/merge",
            post(branch_handlers::merge_branch),
        )
        .route(
            "/streams/{id}/branches/{name}/archive",
            post(branch_handlers::archive_branch),
        )
        // schema (Bloque E2)
        .route(
            "/streams/{id}/schema/validate",
            post(schema_handlers::validate_schema),
        )
        .route(
            "/streams/{id}/schema/history",
            get(schema_handlers::list_schema_history),
        )
        .with_state(state)
        .layer(middleware::from_fn_with_state(jwt_config, auth_layer))
        .layer(audit_trail::middleware::audit_layer());

    Router::new().nest("/api/v1/streaming", api)
}

/// Load the routing table from `topic-routes.yaml` and connect the
/// backends it references. When the routing file is missing the service
/// boots with an empty table — only the REST control plane will be
/// operational and gRPC publishes will return INVALID_ARGUMENT.
async fn build_routing_runtime(
    cfg: &AppConfig,
) -> Result<(Arc<RouteTable>, Arc<BackendRegistry>), Box<dyn std::error::Error>> {
    if !Path::new(&cfg.routes_file).exists() {
        tracing::warn!(
            routes_file = %cfg.routes_file,
            "topic-routes.yaml not found; gRPC routing facade will start with an empty table"
        );
        let table = Arc::new(RouteTable::new(Vec::new(), None));
        return Ok((table, Arc::new(BackendRegistry::new())));
    }

    let routes = RouterConfig::load(&cfg.routes_file)?;
    let table = Arc::new(routes.compile()?);

    let mut registry = BackendRegistry::new();
    if let Some(nats_cfg) = &routes.backends.nats {
        let backend = NatsBackend::connect(&nats_cfg.url).await?;
        registry.insert(Arc::new(backend));
        tracing::info!(url = %nats_cfg.url, "NATS backend connected");
    }
    if let Some(_kafka_cfg) = &routes.backends.kafka {
        match build_kafka_backend(_kafka_cfg) {
            Ok(backend) => {
                registry.insert(backend);
                tracing::info!("Kafka backend connected via rdkafka");
            }
            Err(reason) => {
                registry.insert(Arc::new(KafkaUnavailableBackend::new()));
                tracing::warn!(
                    reason = %reason,
                    "Kafka backend is configured but the real rdkafka integration is not active. Publishes will return UNAVAILABLE."
                );
            }
        }
    }
    Ok((table, Arc::new(registry)))
}

async fn healthz() -> (StatusCode, &'static str) {
    (StatusCode::OK, "ok")
}

/// Build the hot buffer used by `push_events` and `create_stream`.
///
/// Selection order:
/// 1. If the `kafka-rdkafka` feature is compiled in *and*
///    `kafka_bootstrap_servers` is set in the config, build
///    [`KafkaHotBuffer`].
/// 2. Otherwise, derive the NATS URL from the routing table's NATS
///    backend (so the hot buffer reuses the same broker as the gRPC
///    facade) or fall back to `nats://nats:4222`.
/// 3. If both attempts fail (no NATS reachable), drop down to
///    [`NoopHotBuffer`] so the REST control plane still boots.
async fn build_hot_buffer(cfg: &AppConfig) -> Arc<dyn HotBuffer> {
    #[cfg(not(feature = "kafka-rdkafka"))]
    let _ = cfg;
    #[cfg(feature = "kafka-rdkafka")]
    {
        if cfg
            .kafka_bootstrap_servers
            .as_deref()
            .map(|s| !s.trim().is_empty())
            .unwrap_or(false)
        {
            match KafkaHotBuffer::from_env("event-streaming-service") {
                Ok(buffer) => return Arc::new(buffer),
                Err(err) => tracing::warn!(
                    error = %err,
                    "kafka hot buffer unavailable; falling back to NATS"
                ),
            }
        }
    }

    let nats_url = std::env::var("NATS_URL").unwrap_or_else(|_| "nats://nats:4222".to_string());
    match NatsHotBuffer::connect(&nats_url).await {
        Ok(buffer) => Arc::new(buffer),
        Err(err) => {
            tracing::warn!(
                error = %err,
                url = %nats_url,
                "NATS hot buffer unavailable; falling back to noop"
            );
            Arc::new(NoopHotBuffer)
        }
    }
}

/// Build a real Kafka backend when the `kafka-rdkafka` Cargo feature is
/// compiled in **and** `KAFKA_BOOTSTRAP_SERVERS` is set. Otherwise return an
/// `Err` so `main` can fall back to the unavailable stub.
#[cfg(feature = "kafka-rdkafka")]
fn build_kafka_backend(
    kafka_cfg: &event_streaming_service::router::config::KafkaBackendConfig,
) -> Result<Arc<dyn event_streaming_service::backends::Backend>, String> {
    use event_bus_data::config::{DataBusConfig, ServicePrincipal};
    let env_brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS").ok();
    let brokers = match env_brokers {
        Some(v) if !v.is_empty() => v,
        _ if !kafka_cfg.brokers.is_empty() => kafka_cfg.brokers.join(","),
        _ => return Err("no Kafka brokers configured (KAFKA_BOOTSTRAP_SERVERS unset and routing file has empty `brokers`)".to_string()),
    };
    let service = kafka_cfg
        .client_id
        .clone()
        .or_else(|| std::env::var("KAFKA_CLIENT_ID").ok())
        .unwrap_or_else(|| "event-streaming-service".to_string());
    let principal = match (
        std::env::var("KAFKA_SASL_USERNAME").ok(),
        std::env::var("KAFKA_SASL_PASSWORD").ok(),
    ) {
        (Some(_), Some(password)) => ServicePrincipal::scram_sha_512(service.clone(), password),
        _ => ServicePrincipal::insecure_dev(service.clone()),
    };
    let cfg = DataBusConfig::new(brokers, principal);
    let group_prefix = format!("{service}-router");
    RdKafkaBackend::new(cfg, group_prefix)
        .map(|b| Arc::new(b) as Arc<dyn event_streaming_service::backends::Backend>)
        .map_err(|e| e.to_string())
}

#[cfg(not(feature = "kafka-rdkafka"))]
fn build_kafka_backend(
    _kafka_cfg: &event_streaming_service::router::config::KafkaBackendConfig,
) -> Result<Arc<dyn event_streaming_service::backends::Backend>, String> {
    Err("event-streaming-service was built without the `kafka-rdkafka` feature".to_string())
}

async fn metrics_handler(State(metrics): State<Arc<Metrics>>) -> impl IntoResponse {
    match metrics.render() {
        Ok(body) => (
            StatusCode::OK,
            [("content-type", "text/plain; version=0.0.4")],
            body,
        )
            .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("metrics render error: {e}"),
        )
            .into_response(),
    }
}
