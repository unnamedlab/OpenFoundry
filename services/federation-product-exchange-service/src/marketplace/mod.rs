//! `marketplace-service` library.
//!
//! P5 — first non-stub binary. Exposes the full marketplace surface
//! (listings + packages + installs + reviews + devops fleets) plus
//! the new dataset-product endpoints from
//! `migrations/20260503000003_dataset_products.sql`.
//!
//! Mirrors the lib+main split used by every other service in the
//! workspace: `main.rs` instantiates `AppState`, runs migrations,
//! and serves the [`build_router`] output.

use std::sync::Arc;

use axum::{
    Router,
    routing::{get, post},
};
use sqlx::PgPool;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: Arc<auth_middleware::jwt::JwtConfig>,
    pub http_client: reqwest::Client,
    /// Sibling app-builder URL used by `domain::activation` when the
    /// install-runner needs to provision an app template.
    pub app_builder_service_url: String,
}

/// Build the public HTTP router. Routes mount under `/v1/*`; public
/// `/healthz` and `/metrics` are exposed without auth.
pub fn build_router(state: AppState) -> Router {
    use tower_http::trace::TraceLayer;

    let api = Router::new()
        // ── Browse / discovery ───────────────────────────────────────
        .route("/v1/marketplace/overview", get(handlers::browse::get_overview))
        .route(
            "/v1/marketplace/categories",
            get(handlers::browse::list_categories),
        )
        .route("/v1/marketplace/listings", get(handlers::browse::list_listings))
        .route(
            "/v1/marketplace/listings/{id}",
            get(handlers::browse::get_listing),
        )
        .route("/v1/marketplace/search", get(handlers::browse::search_listings))
        // ── Publish (listings + versions) ────────────────────────────
        .route(
            "/v1/marketplace/listings",
            post(handlers::publish::publish_listing),
        )
        .route(
            "/v1/marketplace/listings/{id}",
            axum::routing::patch(handlers::publish::update_listing),
        )
        .route(
            "/v1/marketplace/listings/{id}/versions",
            get(handlers::publish::list_versions).post(handlers::publish::publish_version),
        )
        .route(
            "/v1/marketplace/listings/{id}/actions",
            post(handlers::publish::include_action_in_product),
        )
        // ── Installs ────────────────────────────────────────────────
        .route(
            "/v1/marketplace/installs",
            get(handlers::install::list_installs).post(handlers::install::create_install),
        )
        // P5 — dataset products. Two new endpoints:
        //   POST /v1/products/from-dataset/{rid}  → publish
        //   POST /v1/products/{id}/install        → replay
        // Mirrored under `/v1/marketplace/products/*` so the existing
        // edge-gateway rule that already routes `/api/v1/marketplace/*`
        // can reach them without a new prefix.
        .route(
            "/v1/products/from-dataset/{rid}",
            post(handlers::dataset_product::create_from_dataset),
        )
        .route(
            "/v1/products/{id}",
            get(handlers::dataset_product::get_dataset_product),
        )
        .route(
            "/v1/products/{id}/install",
            post(handlers::dataset_product::install_dataset_product),
        )
        .route(
            "/v1/marketplace/products/from-dataset/{rid}",
            post(handlers::dataset_product::create_from_dataset),
        )
        .route(
            "/v1/marketplace/products/{id}",
            get(handlers::dataset_product::get_dataset_product),
        )
        .route(
            "/v1/marketplace/products/{id}/install",
            post(handlers::dataset_product::install_dataset_product),
        )
        // P3 — Marketplace product schedules.
        .route(
            "/v1/products/{id}/schedules",
            post(handlers::schedule_manifest::add_schedule_manifest),
        )
        .route(
            "/v1/products/{id}/install:schedules",
            post(handlers::schedule_manifest::materialise_install_schedules),
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
