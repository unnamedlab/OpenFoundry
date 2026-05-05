//! `iceberg-catalog-service` — Foundry's implementation of the Apache
//! Iceberg REST Catalog OpenAPI specification.
//!
//! Surface:
//!
//! * [`config`]   — environment-driven configuration loader.
//! * [`domain`]   — typed data model + Postgres CRUD.
//! * [`handlers`] — Axum routers for the REST Catalog spec endpoints
//!                  (`/iceberg/v1/...`), the OAuth2 token surface and
//!                  the Foundry admin endpoints.
//! * [`metrics`]  — Prometheus registry and counters listed in the
//!                  closing-task spec § 9.
//! * [`AppState`] — shared application state injected into every
//!                  handler.
//!
//! The crate is library-first so tests in `tests/` can build a router
//! around an in-memory pool. The `main.rs` binary boots the production
//! server with the same router.

pub mod audit;
pub mod authz;
pub mod build_integration;
pub mod client;
pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod state;

pub use state::{AppState, IcebergState};

use axum::Router;
use axum::routing::{get, head, post};

/// Build the full Axum router for the service. The function is exposed
/// so integration tests can attach the same routes to a `TestServer`
/// without duplicating the table.
pub fn build_router(state: AppState) -> Router {
    let rest_v1 = Router::new()
        // §§ Configuration
        .route(
            "/iceberg/v1/config",
            get(handlers::rest_catalog::config::get_config),
        )
        // §§ Namespaces
        .route(
            "/iceberg/v1/namespaces",
            get(handlers::rest_catalog::namespaces::list_namespaces)
                .post(handlers::rest_catalog::namespaces::create_namespace),
        )
        .route(
            "/iceberg/v1/namespaces/{namespace}",
            get(handlers::rest_catalog::namespaces::load_namespace)
                .delete(handlers::rest_catalog::namespaces::drop_namespace),
        )
        .route(
            "/iceberg/v1/namespaces/{namespace}/properties",
            get(handlers::rest_catalog::namespaces::get_properties)
                .post(handlers::rest_catalog::namespaces::update_properties),
        )
        // §§ Tables
        .route(
            "/iceberg/v1/namespaces/{namespace}/tables",
            get(handlers::rest_catalog::tables::list_tables)
                .post(handlers::rest_catalog::tables::create_table),
        )
        .route(
            "/iceberg/v1/namespaces/{namespace}/tables/{table}",
            get(handlers::rest_catalog::tables::load_table)
                .post(handlers::rest_catalog::tables::commit_table)
                .delete(handlers::rest_catalog::tables::drop_table),
        )
        .route(
            "/iceberg/v1/namespaces/{namespace}/tables/{table}",
            head(handlers::rest_catalog::tables::table_exists),
        )
        // §§ Explicit ALTER TABLE for schema strict-mode (P2).
        .route(
            "/iceberg/v1/namespaces/{namespace}/tables/{table}/alter-schema",
            post(handlers::rest_catalog::tables::alter_schema),
        )
        // §§ Multi-table commit (P2 atomic semantics).
        .route(
            "/iceberg/v1/transactions/commit",
            post(handlers::rest_catalog::transactions::multi_table_commit),
        )
        // §§ Markings (P3) — namespace + table.
        .route(
            "/iceberg/v1/namespaces/{namespace}/markings",
            get(handlers::markings::get_namespace_markings)
                .post(handlers::markings::update_namespace_markings),
        )
        .route(
            "/iceberg/v1/namespaces/{namespace}/tables/{table}/markings",
            get(handlers::markings::get_table_markings)
                .patch(handlers::markings::update_table_markings),
        )
        // §§ Connection diagnose (P3 — UI Catalog Access tab).
        .route(
            "/iceberg/v1/diagnose",
            post(handlers::diagnose::run_diagnose),
        );

    // OAuth2 + Bearer surfaces. Per spec the `/oauth/tokens` endpoint
    // lives **inside** the Iceberg API surface so PyIceberg can perform
    // the token exchange against the same base URI.
    let oauth = Router::new()
        .route(
            "/iceberg/v1/oauth/tokens",
            post(handlers::auth::oauth::issue_token),
        )
        .route(
            "/v1/iceberg-clients/api-tokens",
            post(handlers::auth::api_tokens::create_api_token),
        );

    // Foundry admin surface (UI-facing): list/show iceberg tables, etc.
    let admin = Router::new()
        .route(
            "/api/v1/iceberg-tables",
            get(handlers::admin::list_iceberg_tables),
        )
        .route(
            "/api/v1/iceberg-tables/{id}",
            get(handlers::admin::get_iceberg_table_detail),
        )
        .route(
            "/api/v1/iceberg-tables/{id}/snapshots",
            get(handlers::admin::list_iceberg_table_snapshots),
        )
        .route(
            "/api/v1/iceberg-tables/{id}/metadata",
            get(handlers::admin::get_iceberg_table_metadata),
        )
        .route(
            "/api/v1/iceberg-tables/{id}/branches",
            get(handlers::admin::list_iceberg_table_branches),
        );

    Router::new()
        .merge(rest_v1)
        .merge(oauth)
        .merge(admin)
        .route("/health", get(|| async { "ok" }))
        .route("/metrics", get(metrics::render_metrics))
        .with_state(state)
}

pub mod testing {
    //! Helpers shared by integration tests. Not part of the public API.
    pub use crate::handlers::auth::bearer::issue_internal_jwt;
}
