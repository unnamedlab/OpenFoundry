//! `dataset-quality-service` library.
//!
//! P6 — first non-stub binary. Exposes the existing column-profiling
//! / lint / rules surface plus the new `dataset_health` aggregate
//! consumed by the U4 QualityDashboard cards.

use std::sync::Arc;

use axum::{
    Router,
    routing::{get, post},
};
use sqlx::PgPool;
use storage_abstraction::StorageBackend;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub storage: Arc<dyn StorageBackend>,
    pub jwt_config: Arc<auth_middleware::jwt::JwtConfig>,
}

/// Build the public HTTP router. Routes mount under `/api/v1/*` to
/// match the gateway proxy + the existing handler URL contract.
pub fn build_router(state: AppState) -> Router {
    use tower_http::trace::TraceLayer;

    crate::metrics::init();

    let api = Router::new()
        // ── Quality (existing) ───────────────────────────────────────
        .route(
            "/api/v1/datasets/{id}/quality",
            get(handlers::quality::get_dataset_quality),
        )
        .route(
            "/api/v1/datasets/{id}/quality/profile",
            post(handlers::quality::refresh_dataset_quality),
        )
        .route(
            "/api/v1/datasets/{id}/quality/rules",
            post(handlers::quality::create_quality_rule),
        )
        .route(
            "/api/v1/datasets/{id}/quality/rules/{rule_id}",
            axum::routing::patch(handlers::quality::update_quality_rule)
                .delete(handlers::quality::delete_quality_rule),
        )
        // ── Lint (existing) ──────────────────────────────────────────
        .route(
            "/api/v1/datasets/{id}/lint",
            get(handlers::lint::get_dataset_lint),
        )
        // ── P6 — health snapshot consumed by the dashboard. RID-keyed
        //   so the UI can pass either a UUID or a textual RID.
        .route(
            "/v1/datasets/{rid}/health",
            get(handlers::health::get_dataset_health),
        )
        .route(
            "/api/v1/datasets/{rid}/health",
            get(handlers::health::get_dataset_health),
        );

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
