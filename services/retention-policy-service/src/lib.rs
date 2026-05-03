//! `retention-policy-service` library — exposes the domain, models,
//! handlers and shared `AppState` plus a Foundry-flavoured HTTP
//! [`build_router`] consumed by the binary in `src/main.rs`.
//!
//! P4 — Foundry "View retention policies for a dataset [Beta]" surface.
//! `Datasets.md` § "Retention" puts the cleanup-of-physical-files
//! contract under this service: `dataset-versioning-service` only
//! marks files as removed-from-view, while *this* service is the one
//! that emits cleanup events that physically purge after the policy's
//! grace period.

use std::sync::Arc;

use axum::{Router, routing::get};
use sqlx::PgPool;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: Arc<auth_middleware::jwt::JwtConfig>,
}

/// Build the public HTTP router: policy CRUD, applicable policies for
/// a dataset (with inheritance + winner resolution), retention preview,
/// jobs surface, plus public `/healthz` and `/metrics` endpoints.
pub fn build_router(state: AppState) -> Router {
    use tower_http::trace::TraceLayer;

    crate::metrics::init();

    let api = Router::new()
        // ── Policy CRUD ──────────────────────────────────────────────
        // The bare `/v1/policies` route is kept for legacy callers (the
        // pre-P4 surface).
        .route(
            "/v1/policies",
            get(handlers::retention::list_policies).post(handlers::retention::create_policy),
        )
        .route(
            "/v1/policies/{id}",
            get(handlers::retention::get_policy)
                .put(handlers::retention::update_policy)
                .delete(handlers::retention::delete_policy),
        )
        // P4 — namespaced surface used by the gateway. Mirrors the
        // `/api/v1/retention/*` proxy entry so retention CRUD doesn't
        // collide with `authorization-policy-service`'s RBAC `/policies`.
        .route(
            "/v1/retention/policies",
            get(handlers::retention::list_policies).post(handlers::retention::create_policy),
        )
        .route(
            "/v1/retention/policies/{id}",
            get(handlers::retention::get_policy)
                .put(handlers::retention::update_policy)
                .delete(handlers::retention::delete_policy),
        )
        // ── Jobs ─────────────────────────────────────────────────────
        .route(
            "/v1/jobs",
            get(handlers::retention::list_jobs).post(handlers::retention::run_job),
        )
        // ── Dataset / transaction-scoped views ───────────────────────
        .route(
            "/v1/datasets/{dataset_id}/retention",
            get(handlers::retention::get_dataset_retention),
        )
        .route(
            "/v1/transactions/{transaction_id}/retention",
            get(handlers::retention::get_transaction_retention),
        )
        // ── P4 — applicable policies + preview ───────────────────────
        // `applicable-policies` resolves the inheritance chain (Org →
        // Space → Project → dataset) and surfaces the winning ("most
        // restrictive") policy. `retention-preview` simulates which
        // transactions / files would be purged if the runner fired now
        // (or in `as_of_days` days).
        .route(
            "/v1/datasets/{rid}/applicable-policies",
            get(handlers::retention::applicable_policies),
        )
        .route(
            "/v1/datasets/{rid}/retention-preview",
            get(handlers::retention::retention_preview),
        );

    // PUT support is mounted via `.put(update_policy)` chained on the
    // `/v1/policies/{id}` route above, so no separate route is needed.

    // Public surface (no JWT layer): health + metrics scrape.
    let public = Router::new()
        .route("/healthz", get(handlers::health::healthz))
        .route("/health", get(handlers::health::healthz))
        .route("/metrics", get(handlers::health::metrics));

    Router::new()
        .merge(api)
        .merge(public)
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}
