//! `dataset-versioning-service` — biblioteca compartida.
//!
//! Expone:
//! * [`AppState`]: estado inyectado en cada handler (PgPool, JwtConfig,
//!   `DatasetWriter`, cliente HTTP y URLs de servicios vecinos).
//! * [`build_router`]: construye el `axum::Router` con autenticación,
//!   audit logging básico, métricas Prometheus y todos los endpoints
//!   definidos en `handlers/`.
//!
//! El binario en `src/main.rs` se limita a leer la configuración, abrir el
//! pool de Postgres, ejecutar las migraciones y arrancar el listener TCP.

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use sqlx::PgPool;

pub use crate::storage::DatasetWriter;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;
pub mod storage;

/// Estado compartido por todos los handlers.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: Arc<JwtConfig>,
    pub writer: Arc<dyn DatasetWriter>,
    pub http: reqwest::Client,
    pub retention_policy_url: Option<String>,
    pub data_asset_catalog_url: Option<String>,
}

/// Construye el router HTTP completo del servicio.
pub fn build_router(state: AppState) -> Router {
    use axum::middleware::from_fn_with_state;
    use axum::routing::{get, post};
    use tower_http::trace::TraceLayer;

    // Sub-router protegido por JWT con la API pública del servicio.
    let api = Router::new()
        // ── Branches (Foundry-style) ──────────────────────────────────────
        .route(
            "/v1/datasets/{rid}/branches",
            get(handlers::foundry::list_branches).post(handlers::foundry::create_branch),
        )
        // GET /branches/{branch}, DELETE /branches/{branch} y
        // POST /branches/{branch}:reparent comparten patrón de path; Axum
        // los discrimina por método. El handler POST parsea la acción.
        .route(
            "/v1/datasets/{rid}/branches/{branch}",
            get(handlers::foundry::get_branch)
                .delete(handlers::foundry::delete_branch)
                .post(handlers::foundry::branch_action),
        )
        // ── Transactions ──────────────────────────────────────────────────
        .route(
            "/v1/datasets/{rid}/branches/{branch}/transactions",
            post(handlers::foundry::start_transaction),
        )
        // GET /transactions/{txn} (lookup) y
        // POST /transactions/{txn}:commit | {txn}:abort
        .route(
            "/v1/datasets/{rid}/branches/{branch}/transactions/{txn}",
            get(handlers::foundry::get_transaction)
                .post(handlers::foundry::transaction_action),
        )
        // ── Fallback chain (T2.3) ─────────────────────────────────────────
        .route(
            "/v1/datasets/{rid}/branches/{branch}/fallbacks",
            get(handlers::foundry::list_fallbacks).put(handlers::foundry::put_fallbacks),
        )
        // Listado plano por dataset con filtros ?branch=&before=
        .route(
            "/v1/datasets/{rid}/transactions",
            get(handlers::foundry::list_transactions),
        )
        // ── Views ─────────────────────────────────────────────────────────
        .route(
            "/v1/datasets/{rid}/views/current",
            get(handlers::foundry::get_current_view),
        )
        .route(
            "/v1/datasets/{rid}/views/at",
            get(handlers::foundry::get_view_at),
        )
        .route(
            "/v1/datasets/{rid}/views/{view_id}/files",
            get(handlers::foundry::list_view_files),
        )
        .layer(from_fn_with_state(
            (*state.jwt_config).clone(),
            auth_middleware::layer::auth_layer,
        ));

    // Endpoints públicos (sin auth): salud y métricas Prometheus.
    let public = Router::new()
        .route("/healthz", get(handlers::health::healthz))
        .route("/health", get(handlers::health::healthz))
        .route("/metrics", get(handlers::health::metrics));

    // Pre-register the `dataset_*` metric families so /metrics scrapes
    // see the prefix immediately (smoke test contract).
    crate::metrics::init();

    Router::new()
        .merge(api)
        .merge(public)
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}
