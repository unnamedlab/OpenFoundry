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
//!
//! Ownership boundary:
//! * `dataset_versions`, `dataset_branches` y `dataset_transactions`
//!   pertenecen a este servicio.
//! * Iceberg es la fuente de verdad para snapshots y dataset data state.
//! * Postgres en este bounded context conserva sólo metadata declarativa
//!   y coordinación transaccional mínima del runtime.

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use sqlx::PgPool;
use storage_abstraction::StorageBackend;
use storage_abstraction::backing_fs::BackingFileSystem;

pub use crate::storage::DatasetWriter;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;
pub mod security;
pub mod storage;

/// Estado compartido por todos los handlers.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: Arc<JwtConfig>,
    pub writer: Arc<dyn DatasetWriter>,
    /// Read-only storage backend used by the preview path
    /// (`handlers::foundry::preview_view`). Distinct from `writer` so
    /// the preview layer can `get` raw bytes without going through the
    /// writer's transactional surface.
    pub storage: Arc<dyn StorageBackend>,
    /// P3 — Foundry-style "Backing filesystem" handle. Owns the
    /// `logical_path → physical_uri` mapping plus presigned URL
    /// generation for the Files tab download / upload routes.
    pub backing_fs: Arc<dyn BackingFileSystem>,
    /// Presigned URL TTL surfaced on every download/upload-url
    /// response. Mirrors `BackingFsConfig::presign_ttl_seconds` so the
    /// audit log lines up with what was actually issued.
    pub presign_ttl_seconds: u64,
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
        .route(
            "/v1/datasets/{rid}/versions",
            get(handlers::versions::list_versions),
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
        .route(
            "/v1/datasets/{rid}/branches/{branch}/checkout",
            post(handlers::branches::checkout_branch),
        )
        // P1 — ancestry walk (child → root).
        .route(
            "/v1/datasets/{rid}/branches/{branch}/ancestry",
            get(handlers::foundry::branch_ancestry),
        )
        // P3 — preview the soft-delete plan (children to be re-parented,
        // transactions preserved). Powers the UI confirm dialog.
        .route(
            "/v1/datasets/{rid}/branches/{branch}/preview-delete",
            get(handlers::foundry::preview_delete_branch),
        )
        // P4 — branch retention + markings inheritance.
        .route(
            "/v1/datasets/{rid}/branches/{branch}/retention",
            axum::routing::patch(handlers::retention::update_retention),
        )
        .route(
            "/v1/datasets/{rid}/branches/{branch}/markings",
            get(handlers::retention::get_branch_markings),
        )
        .route(
            "/v1/datasets/{rid}/branches/{branch}:restore",
            post(handlers::retention::restore_branch),
        )
        // P5 — branch comparison: LCA + diverged transactions +
        // conflicting files. Powers `BranchCompare.svelte`.
        .route(
            "/v1/datasets/{rid}/branches/compare",
            get(handlers::compare::compare_branches),
        )
        .route(
            "/v1/datasets/{rid}/branches/{branch}/rollback",
            post(handlers::foundry::rollback_branch),
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
        // P6 — Application reference: 207 Multi-Status batch endpoint.
        // Body: `{ "ids": ["<txn_uuid>", ...] }`. Returns one row per
        // input id with the per-row status it would have produced as a
        // single GET (200 / 404 / 400 for bad uuids).
        .route(
            "/v1/datasets/{rid}/transactions:batchGet",
            post(handlers::foundry::batch_get_transactions),
        )
        .route(
            "/v1/datasets/{rid}/compare",
            get(handlers::foundry::compare_views),
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
        // ── View-scoped schemas (Foundry parity) ──────────────────────────
        // GET returns the schema persisted for a specific view; POST is
        // idempotent on content hash. The legacy alias
        // `GET /v1/datasets/{rid}/schema` resolves the current view first.
        .route(
            "/v1/datasets/{rid}/views/{view_id}/schema",
            get(handlers::schema::get_view_schema).post(handlers::schema::put_view_schema),
        )
        // P2 — Foundry view preview (file-list views with file_format
        // dispatch). Sits at `/data` rather than `/preview` because
        // `data-asset-catalog-service` already owns `/views/{view}/preview`
        // for materialised SQL views, and the two concepts are distinct.
        .route(
            "/v1/datasets/{rid}/views/{view_id}/data",
            get(handlers::preview::preview_view),
        )
        // P3 — Backing-filesystem files surface. View-effective list +
        // 302 download + presigned upload-URL for the Files tab.
        .route(
            "/v1/datasets/{rid}/files",
            get(handlers::files::list_files),
        )
        .route(
            "/v1/datasets/{rid}/files/{file_id}/download",
            get(handlers::files::download_file),
        )
        .route(
            "/v1/datasets/{rid}/transactions/{txn_id}/files",
            post(handlers::files::upload_url),
        )
        .route(
            "/v1/datasets/{rid}/storage-details",
            get(handlers::files::storage_details),
        )
        .route(
            "/v1/datasets/{rid}/schema",
            get(handlers::schema::get_current_schema),
        )
        .layer(from_fn_with_state(
            (*state.jwt_config).clone(),
            auth_middleware::layer::auth_layer,
        ));

    // Endpoints públicos (sin auth): salud y métricas Prometheus.
    //
    // P3 — `/v1/_internal/local-fs/{*key}` is the LocalBackingFs
    // presign proxy. It's intentionally outside the JWT layer because
    // the HMAC in the query string (signed by the service secret) is
    // the authenticator: the URL itself carries proof-of-grant.
    let public = Router::new()
        .route("/healthz", get(handlers::health::healthz))
        .route("/health", get(handlers::health::healthz))
        .route("/metrics", get(handlers::health::metrics))
        .route(
            "/v1/_internal/local-fs/{*key}",
            get(handlers::files::local_presign_proxy),
        )
        .with_state(state.clone());

    // Pre-register the `dataset_*` metric families so /metrics scrapes
    // see the prefix immediately (smoke test contract).
    crate::metrics::init();

    Router::new()
        .merge(api)
        .merge(public)
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}
