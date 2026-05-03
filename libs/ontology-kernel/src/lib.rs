//! Shared ontology kernel: configuration, domain logic, models and HTTP
//! handlers reused by every `ontology-*` and `object-database-service` crate.
//!
//! Historically this tree was injected into each service via
//! `#[path = "../../../../libs/ontology-kernel/src/.../mod.rs"]`. It is now a
//! real Cargo crate so it can be linted, tested and consumed via
//! `use ontology_kernel::handlers::actions::*;` etc.

pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;
pub mod stores;

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

/// Shared application state used by every handler in the kernel.
///
/// Each ontology-* binary builds an [`AppState`] from environment configuration
/// and wires it through `axum::Router::with_state`. The kernel handlers consume
/// it via `axum::extract::State<AppState>` and only depend on the public fields
/// declared below.
#[derive(Clone)]
pub struct AppState {
    /// PostgreSQL pool retained for control-plane schema lookups, outbox, and
    /// residual warm handlers that have not migrated off direct PG access yet.
    /// The object/link/action hot path routes through [`Self::stores`].
    pub db: PgPool,
    /// Storage trait bag — see [`stores::Stores`]. Handlers migrated as
    /// part of S1.4–S1.7 of the Cassandra-Foundry parity plan route their
    /// I/O through this field; legacy handlers still use [`Self::db`]
    /// directly. Both fields coexist while the migration is in flight.
    pub stores: stores::Stores,
    pub http_client: reqwest::Client,
    pub jwt_config: JwtConfig,
    pub audit_service_url: String,
    pub dataset_service_url: String,
    pub ontology_service_url: String,
    pub pipeline_service_url: String,
    pub ai_service_url: String,
    pub notification_service_url: String,
    pub search_embedding_provider: String,
    pub node_runtime_command: String,
    /// Base URL of `connector-management-service`. Used by TASK G to invoke
    /// registered webhooks (writeback + side effects). When empty, the kernel
    /// logs a warning and skips the call.
    #[doc(hidden)]
    pub connector_management_service_url: String,
}

#[cfg(test)]
pub(crate) mod test_support {
    pub fn lazy_pg_pool() -> sqlx::PgPool {
        sqlx::postgres::PgPoolOptions::new()
            .connect_lazy("postgres://openfoundry:openfoundry@127.0.0.1:1/openfoundry")
            .expect("lazy test pool")
    }
}
