//! `ontology-definition-service` — schema-of-types domain.
//!
//! Per the Cassandra-Foundry parity plan (S1.6), this service keeps
//! the declarative ontology schema boundary in **Postgres**, on the
//! consolidated `pg-schemas` cluster, schema `ontology_schema`:
//! object types, link types, action types, properties, interfaces,
//! shared property types and ontology projects.
//! Cassandra is reserved for hot-path object/link state owned by
//! `object-database-service`, `ontology-actions-service` and
//! `ontology-query-service`. Runtime rows such as `object_instances`,
//! `link_instances` and execution/run ledgers are intentionally out
//! of scope here.
//!
//! This crate is the substrate shell:
//!
//! * [`config::AppConfig`] — env-driven, defaults to `pg-schemas`.
//! * [`db::build_pool`] — sqlx pool with `search_path=ontology_schema`
//!   applied via `PgConnectOptions::options` (S1.6.b / S1.6.d).
//! * [`schema_events::SchemaPublisher`] — `ontology.schema.v1`
//!   publisher used by handlers to invalidate downstream caches
//!   (S1.6.e).
//! * [`build_router`] — minimal `/api/v1/ontology-definition` router.
//!
//! Migrating the kernel handlers (`libs/ontology-kernel/src/handlers/
//! types.rs`, `links.rs`, `actions.rs`, …) to publish the schema event
//! at write-time is tracked as a per-handler follow-up — same pattern
//! used in S1.4.b / S1.5.f.

pub mod config;
pub mod db;
pub mod schema_events;

use std::sync::Arc;

use axum::Router;
use axum::routing::get;

/// Shared application state injected into Axum handlers.
#[derive(Clone)]
pub struct AppState {
    pub db: Option<sqlx::PgPool>,
    pub publisher: schema_events::SchemaPublisher,
    pub config: Arc<config::AppConfig>,
}

/// Minimal router that the binary mounts. Kernel handlers are wired
/// in their per-PR migration; this only exposes the boundary surface
/// needed by smoke tests and operators today.
pub fn build_router(state: AppState) -> Router {
    Router::new()
        .route("/health", get(|| async { "ok" }))
        .route("/api/v1/ontology-definition/health", get(handlers::health))
        .with_state(state)
}

mod handlers {
    use super::AppState;
    use axum::{Json, extract::State, http::StatusCode, response::IntoResponse};

    pub async fn health(State(state): State<AppState>) -> impl IntoResponse {
        let db_ready = match state.db.as_ref() {
            Some(pool) => matches!(sqlx::query("SELECT 1").execute(pool).await, Ok(_)),
            None => false,
        };
        let body = serde_json::json!({
            "status": "ok",
            "database": if db_ready { "ready" } else { "disabled" },
            "schema": state.config.pg_schema,
            "events": if state.publisher.is_enabled() { "enabled" } else { "disabled" },
        });
        (StatusCode::OK, Json(body))
    }
}
