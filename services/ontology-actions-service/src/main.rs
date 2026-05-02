//! `ontology-actions-service` binary entry point.
//!
//! Owns the writeback surface of the Action types feature
//! (`/api/v1/ontology/actions/*` and the per-property inline-edit route).
//! All business logic lives in `libs/ontology-kernel::handlers::actions`;
//! this binary just wires configuration, the Postgres pool, the JWT layer
//! and the Axum router built by [`ontology_actions_service::build_router`].
//!
//! ## S1.4 wiring
//!
//! * **DI** ([S1.4.a]): the [`AppState::stores`] field carries a
//!   [`Stores`](ontology_kernel::stores::Stores) bag with
//!   `Arc<dyn ObjectStore>` plus the link/action-log siblings. When
//!   `CASSANDRA_CONTACT_POINTS` is set, the binary instantiates the
//!   Cassandra-backed stores and warms their prepared statements at
//!   startup; otherwise it falls back to the in-memory noop stores
//!   (smoke tests, local dev without a Cassandra cluster).
//! * **sqlx scope** ([S1.4.e]): the `PgPool` survives in this binary
//!   *only* to back [`outbox::enqueue`] calls inside
//!   [`ontology_kernel::domain::writeback::apply_object_with_outbox`]
//!   and the kernel handlers that have not yet migrated. The
//!   service no longer applies `sqlx::migrate!` against its own
//!   tree — the legacy DDL has been archived under
//!   `docs/architecture/legacy-migrations/ontology-actions-service/`
//!   ([S1.4.d]) and any residual schema is provisioned by the
//!   `pg-policy` cluster owner (`outbox.events`) plus the
//!   `pg-schemas` consolidation that lands in S1.6.

use std::net::SocketAddr;
use std::sync::Arc;

use axum::{
    Router,
    extract::Extension,
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::get,
};
use cassandra_kernel::{
    ClusterConfig, Migration, SessionBuilder, migrate,
    repos::{CassandraActionLogStore, CassandraLinkStore, CassandraObjectStore},
};
use ontology_actions_service::{build_router, config::AppConfig, jwt_config_from_secret};
use ontology_kernel::AppState;
use sqlx::postgres::PgPoolOptions;
use tower_http::trace::TraceLayer;
use tracing_subscriber::EnvFilter;

const ONTOLOGY_OBJECTS_MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "ontology_object_tables",
    statements: &[
        include_str!("../../object-database-service/cql/ontology_objects/001_objects_by_id.cql"),
        include_str!("../../object-database-service/cql/ontology_objects/002_objects_by_type.cql"),
        include_str!("../../object-database-service/cql/ontology_objects/003_objects_by_owner.cql"),
        include_str!(
            "../../object-database-service/cql/ontology_objects/004_objects_by_marking.cql"
        ),
    ],
}];

const ONTOLOGY_INDEXES_MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "ontology_link_indexes",
    statements: &[
        include_str!("../../object-database-service/cql/ontology_indexes/001_links_outgoing.cql"),
        include_str!("../../object-database-service/cql/ontology_indexes/002_links_incoming.cql"),
    ],
}];

const ACTIONS_LOG_MIGRATIONS: &[Migration] = &[
    Migration {
        version: 1,
        name: "actions_log_table",
        statements: &[include_str!("../cql/actions_log/001_actions_log.cql")],
    },
    Migration {
        version: 2,
        name: "actions_by_object_index",
        statements: &[include_str!("../cql/actions_log/002_actions_by_object.cql")],
    },
];

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("ontology_actions_service=info,ontology_kernel=info,tower_http=info")
        }))
        .init();

    let app_config = AppConfig::from_env()?;

    // `pg-policy` pool. Used only by `outbox::enqueue` (and the
    // residual handler queries that have not yet been migrated to
    // `cassandra-kernel`). No `sqlx::migrate!` call: the outbox DDL
    // is owned by `libs/outbox/migrations/` and applied by the
    // platform DBA, and every other table that lived in this tree
    // was archived under `docs/architecture/legacy-migrations/`.
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;

    let stores = build_stores(&app_config).await?;

    let state = AppState {
        db,
        stores,
        http_client: reqwest::Client::new(),
        jwt_config: jwt_config_from_secret(&app_config.jwt_secret),
        audit_service_url: app_config.audit_service_url.clone(),
        dataset_service_url: app_config.dataset_service_url.clone(),
        ontology_service_url: app_config.ontology_service_url.clone(),
        pipeline_service_url: app_config.pipeline_service_url.clone(),
        ai_service_url: app_config.ai_service_url.clone(),
        notification_service_url: app_config.notification_service_url.clone(),
        search_embedding_provider: app_config.search_embedding_provider.clone(),
        node_runtime_command: app_config.node_runtime_command.clone(),
        connector_management_service_url: app_config.connector_management_service_url.clone(),
    };

    let registry = Arc::new(prometheus::Registry::new());
    ontology_kernel::metrics::register_action_metrics(&registry);

    let app = Router::new()
        .merge(build_router(state))
        .route("/health", get(|| async { "ok" }))
        .route("/metrics", get(render_metrics))
        .layer(Extension(registry))
        .layer(TraceLayer::new_for_http());

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%addr, "starting ontology-actions-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

/// Build the [`Stores`](ontology_kernel::stores::Stores) bag honouring
/// the runtime configuration. When `CASSANDRA_CONTACT_POINTS` is set
/// the bag wires real Cassandra-backed object, link and action-log
/// stores; otherwise it falls back to the in-memory implementations so
/// smoke tests and local `cargo run` keep working without infrastructure.
async fn build_stores(
    cfg: &AppConfig,
) -> Result<ontology_kernel::stores::Stores, Box<dyn std::error::Error>> {
    if cfg.cassandra_contact_points.trim().is_empty() {
        tracing::warn!(
            "CASSANDRA_CONTACT_POINTS not set — falling back to in-memory ObjectStore. \
             Production deployments MUST set this variable (S1.4.a)."
        );
        return Ok(ontology_kernel::stores::Stores::in_memory());
    }

    let cluster = ClusterConfig {
        contact_points: cfg
            .cassandra_contact_points
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect(),
        local_datacenter: cfg.cassandra_local_dc.clone(),
        ..ClusterConfig::dev_local()
    };
    let session = Arc::new(SessionBuilder::new(cluster).build().await?);
    apply_cassandra_schema(session.as_ref()).await?;

    let object_store = CassandraObjectStore::new(session.clone());
    object_store.warm_up().await?;
    let link_store = CassandraLinkStore::new(session.clone());
    link_store.warm_up().await?;
    let action_store = CassandraActionLogStore::new(session.clone());
    action_store.warm_up().await?;

    let mut bag = ontology_kernel::stores::Stores::in_memory();
    bag.objects = Arc::new(object_store);
    bag.links = Arc::new(link_store);
    bag.actions = Arc::new(action_store);
    Ok(bag)
}

async fn apply_cassandra_schema(
    session: &cassandra_kernel::scylla::Session,
) -> Result<(), Box<dyn std::error::Error>> {
    migrate::apply(session, "ontology_objects", ONTOLOGY_OBJECTS_MIGRATIONS).await?;
    migrate::apply(session, "ontology_indexes", ONTOLOGY_INDEXES_MIGRATIONS).await?;
    migrate::apply(session, "actions_log", ACTIONS_LOG_MIGRATIONS).await?;
    Ok(())
}

/// Prometheus exposition endpoint. The registry is shared via an Axum
/// `Extension` so the route remains state-agnostic and can sit alongside
/// the kernel's `Router<AppState>` returned by [`build_router`]. Counters
/// will be populated when TASK F (Action metrics) lands.
async fn render_metrics(Extension(registry): Extension<Arc<prometheus::Registry>>) -> Response {
    use prometheus::Encoder;
    let encoder = prometheus::TextEncoder::new();
    let mut buffer = Vec::new();
    if let Err(error) = encoder.encode(&registry.gather(), &mut buffer) {
        tracing::error!(?error, "failed to encode prometheus metrics");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    (
        StatusCode::OK,
        [(axum::http::header::CONTENT_TYPE, encoder.format_type())],
        buffer,
    )
        .into_response()
}
