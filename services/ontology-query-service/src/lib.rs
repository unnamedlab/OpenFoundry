//! `ontology-query-service` library surface.
//!
//! Owns the **read path** of the Foundry-parity ontology plane
//! ([migration plan §S1.5](../../docs/architecture/migration-plan-cassandra-foundry-parity.md)).
//! All hot-path reads go through an `Arc<dyn ObjectStore>` that the
//! binary wires to a [`CachingObjectStore`] over a
//! [`CassandraObjectStore`](cassandra_kernel::repos::CassandraObjectStore).
//!
//! The shape mirrors `ontology-actions-service`: a thin Axum router
//! that delegates business logic to functions taking
//! `&dyn ObjectStore`. The kernel handlers under
//! `libs/ontology-kernel/src/handlers/` will migrate one-by-one in
//! follow-up PRs (see the `[~]` annotation on S1.5 for scope notes).

pub mod cache;
pub mod config;
pub mod consistency;
pub mod handlers;
pub mod invalidation;

use std::sync::Arc;

use axum::{Router, routing::get};
use storage_abstraction::repositories::ObjectStore;

/// Shared application state. Intentionally narrow: the read service
/// only needs the object store. Schema and link reads will join the
/// state when their respective handlers migrate.
#[derive(Clone)]
pub struct QueryState {
    pub objects: Arc<dyn ObjectStore>,
}

/// Build the public HTTP surface of `ontology-query-service`.
///
/// The auth layer is intentionally *not* applied here — the binary
/// in `src/main.rs` adds it after merging the `/health` and
/// `/metrics` routes so unauthenticated probes keep working.
pub fn build_router(state: QueryState) -> Router {
    let api = Router::new()
        .route(
            "/objects/{tenant}/{object_id}",
            get(handlers::get_object),
        )
        .route(
            "/objects/{tenant}/by-type/{type_id}",
            get(handlers::list_objects_by_type),
        )
        .with_state(state);

    Router::new().nest("/api/v1/ontology", api)
}
