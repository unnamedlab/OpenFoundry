//! `ontology-security-service` binary entry point.
//!
//! Owns the rule-engine plane of the ontology
//! (`/api/v1/ontology/rules/*`, `/api/v1/ontology/types/{id}/rules`,
//! `/api/v1/ontology/objects/{id}/rule-runs`) per
//! `docs/architecture/services-and-ports.md`. Business logic lives in
//! [`ontology_kernel::handlers::rules`]; this binary wires
//! configuration, the Postgres pool, the JWT layer and the Cassandra
//! `Stores` bag, then mounts the kernel handlers on an Axum router.
//!
//! ## Consolidation status
//!
//! Per `docs/architecture/service-consolidation-map.md`, this crate is
//! slated to merge into `ontology-actions-service` (`merge → actions`,
//! pending). Until that merge lands, the binary stays a thin runtime
//! owner of the rule routes the edge gateway already proxies to it
//! (see `services/edge-gateway-service/src/proxy/service_router.rs`).
//!
//! Note: the legacy main.rs also mounted `/api/v1/ontology/projects*`
//! handlers. Those routes now belong to `tenancy-organizations-service`
//! / `ontology-definition-service` per the gateway service router and
//! are intentionally not re-exposed here.

mod config;

use std::net::SocketAddr;
use std::sync::Arc;

use axum::{
    Router, middleware,
    routing::{get, patch, post},
};
use cassandra_kernel::{
    ClusterConfig, SessionBuilder,
    repos::{CassandraActionLogStore, CassandraLinkStore, CassandraObjectStore},
};
use core_models::{health::HealthStatus, observability};
use ontology_kernel::{AppState, domain::pg_repository::PostgresDefinitionStore, handlers::rules};
use search_abstraction::search_backend_from_env;
use sqlx::{PgPool, postgres::PgPoolOptions};
use storage_abstraction::repositories::SearchBackedObjectSetMaterializationStore;
use tower_http::trace::TraceLayer;

use crate::config::AppConfig;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    observability::init_tracing("ontology-security-service");

    let cfg = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await?;

    let stores = build_stores(&cfg, db.clone()).await?;
    let jwt_config = auth_middleware::jwt::JwtConfig::new(&cfg.jwt_secret).with_env_defaults();

    let state = AppState {
        db,
        stores,
        http_client: reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(10))
            .build()?,
        jwt_config: jwt_config.clone(),
        audit_service_url: cfg.audit_service_url.clone(),
        dataset_service_url: cfg.dataset_service_url.clone(),
        ontology_service_url: cfg.ontology_service_url.clone(),
        pipeline_service_url: cfg.pipeline_service_url.clone(),
        ai_service_url: cfg.ai_service_url.clone(),
        notification_service_url: cfg.notification_service_url.clone(),
        search_embedding_provider: cfg.search_embedding_provider.clone(),
        node_runtime_command: cfg.node_runtime_command.clone(),
        connector_management_service_url: cfg.connector_management_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("ontology-security-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology/rules",
            get(rules::list_rules).post(rules::create_rule),
        )
        .route(
            "/api/v1/ontology/rules/insights",
            get(rules::get_machinery_insights),
        )
        .route(
            "/api/v1/ontology/rules/machinery/queue",
            get(rules::get_machinery_queue),
        )
        .route(
            "/api/v1/ontology/rules/machinery/queue/{id}",
            patch(rules::update_machinery_queue_item),
        )
        .route(
            "/api/v1/ontology/rules/{id}",
            get(rules::get_rule)
                .patch(rules::update_rule)
                .delete(rules::delete_rule),
        )
        .route(
            "/api/v1/ontology/rules/{id}/simulate",
            post(rules::simulate_rule),
        )
        .route("/api/v1/ontology/rules/{id}/apply", post(rules::apply_rule))
        .route(
            "/api/v1/ontology/types/{type_id}/rules",
            get(rules::list_rules_for_object_type),
        )
        .route(
            "/api/v1/ontology/objects/{obj_id}/rule-runs",
            get(rules::list_object_rule_runs),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .merge(public)
        .merge(protected)
        .with_state(state)
        .layer(TraceLayer::new_for_http());

    let addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    tracing::info!(%addr, nats_url = %cfg.nats_url, "starting ontology-security-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;
    Ok(())
}

async fn build_stores(
    cfg: &AppConfig,
    db: PgPool,
) -> Result<ontology_kernel::stores::Stores, Box<dyn std::error::Error>> {
    if cfg.cassandra_contact_points.trim().is_empty() {
        tracing::warn!(
            "CASSANDRA_CONTACT_POINTS not set — falling back to in-memory ObjectStore. \
             Production deployments MUST set this variable."
        );
        let mut stores = ontology_kernel::stores::Stores::in_memory();
        stores.definitions = Arc::new(PostgresDefinitionStore::new(db));
        return Ok(stores);
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

    let object_store = CassandraObjectStore::new(session.clone());
    object_store.warm_up().await?;
    let link_store = CassandraLinkStore::new(session.clone());
    link_store.warm_up().await?;
    let action_store = CassandraActionLogStore::new(session);
    action_store.warm_up().await?;

    let mut bag = ontology_kernel::stores::Stores::in_memory();
    bag.objects = Arc::new(object_store);
    bag.links = Arc::new(link_store);
    bag.actions = Arc::new(action_store);
    bag.definitions = Arc::new(PostgresDefinitionStore::new(db));
    if let Ok(search) = search_backend_from_env() {
        bag.search = search.clone();
        bag.object_set_materializations =
            Arc::new(SearchBackedObjectSetMaterializationStore::new(search));
    } else {
        tracing::warn!(
            "SEARCH_ENDPOINT/SEARCH_BACKEND not configured for ontology-security-service; \
             using in-memory search backend"
        );
    }
    Ok(bag)
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
