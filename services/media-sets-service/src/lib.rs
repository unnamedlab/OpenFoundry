//! `media-sets-service` — Foundry-style **media sets** runtime.
//!
//! A media set is a Foundry-RID-addressed collection of unstructured
//! media files (image / audio / video / document / spreadsheet / email)
//! sharing a common schema. This crate owns:
//!
//! * Media-set definitions, branches and transactional write batches
//!   (Postgres tables `media_sets`, `media_set_branches`,
//!   `media_set_transactions`).
//! * Individual media items with path-deduplication semantics
//!   (`media_items`, partial UNIQUE on live paths).
//! * The byte-side abstraction over an S3-compatible backend
//!   ([`domain::MediaStorage`]).
//!
//! Two surfaces are exposed in the same process:
//!
//! 1. A REST/JSON API mounted by [`build_router`] for browsers and the
//!    Foundry web app.
//! 2. A Tonic gRPC service in [`grpc`] mirroring the contract in
//!    `proto/media_set/`.
//!
//! Both ride on the same [`AppState`] so business logic lives once
//! (the `*_op` helpers in [`handlers`]).

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use authz_cedar::AuthzEngine;
use axum::Router;
use db_pool::DualPool;

pub mod config;
pub mod domain;
pub mod grpc;
pub mod handlers;
pub mod metrics;
pub mod models;
pub mod proto;

pub use domain::{BackendMediaStorage, MediaStorage};

/// Shared state injected into every REST handler and into the gRPC
/// service. Cheap to clone (everything inside is `Arc`-shaped).
#[derive(Clone)]
pub struct AppState {
    pub db: DualPool,
    pub jwt_config: Arc<JwtConfig>,
    pub storage: Arc<dyn MediaStorage>,
    pub presign_ttl_seconds: u64,
    /// Outbound HTTP client used to talk to neighbour services
    /// (currently: connector-management-service for virtual media set
    /// source resolution).
    pub http: reqwest::Client,
    /// Base URL of `connector-management-service`. `None` means virtual
    /// media set download URL resolution will return 503.
    pub connector_service_url: Option<String>,
    /// Cedar policy engine (H3). Loaded with the bundled
    /// `MEDIA_DEFAULT_POLICIES` at boot; production overrides via
    /// the standard `pg-policy.cedar_policies` hot-reload (ADR-0027).
    pub engine: Arc<AuthzEngine>,
    /// Raw HMAC secret used to sign + validate the short-lived JWT
    /// claim embedded in presigned download URLs (H3). Same secret
    /// the rest of the platform uses (`JWT_SECRET`) so the
    /// edge-gateway can validate without a network hop.
    pub presign_secret: Arc<Vec<u8>>,
}

/// Build the public Axum router.
///
/// Layout:
/// * `/healthz`, `/health`, `/metrics` — un-authenticated.
/// * Everything else — JWT-protected via
///   [`auth_middleware::layer::auth_layer`]. Audit metadata is emitted
///   per-request through [`audit_trail::middleware::AuditLayer`].
pub fn build_router(state: AppState) -> Router {
    use axum::middleware::from_fn_with_state;
    use axum::routing::{delete, get, post};
    use tower_http::trace::TraceLayer;

    crate::metrics::init();

    let api = Router::new()
        // ── Media sets ─────────────────────────────────────────────
        .route(
            "/media-sets",
            post(handlers::media_sets::create_media_set)
                .get(handlers::media_sets::list_media_sets),
        )
        .route(
            "/media-sets/{rid}",
            get(handlers::media_sets::get_media_set)
                .delete(handlers::media_sets::delete_media_set),
        )
        .route(
            "/media-sets/{rid}/retention",
            axum::routing::patch(handlers::media_sets::patch_retention),
        )
        // ── Markings (H3) ─────────────────────────────────────────
        .route(
            "/media-sets/{rid}/markings",
            axum::routing::patch(handlers::media_sets::patch_markings),
        )
        .route(
            "/media-sets/{rid}/markings/preview",
            post(handlers::media_sets::preview_markings),
        )
        .route(
            "/items/{rid}/markings",
            axum::routing::patch(handlers::items::patch_item_markings),
        )
        // ── Access patterns + usage (H5) ───────────────────────────
        .route(
            "/media-sets/{rid}/access-patterns",
            post(handlers::access_patterns::register_access_pattern)
                .get(handlers::access_patterns::list_access_patterns),
        )
        .route(
            "/access-patterns/{id}/run",
            get(handlers::access_patterns::run_access_pattern),
        )
        .route(
            "/items/{rid}/access-patterns/{kind}/url",
            get(handlers::access_patterns::item_access_pattern_shortcut),
        )
        .route(
            "/media-sets/{rid}/usage",
            get(handlers::usage::get_usage),
        )
        // ── Branches (H4) ──────────────────────────────────────────
        .route(
            "/media-sets/{rid}/branches",
            post(handlers::branches::create_branch).get(handlers::branches::list_branches),
        )
        .route(
            "/media-sets/{rid}/branches/{name}",
            delete(handlers::branches::delete_branch),
        )
        .route(
            "/media-sets/{rid}/branches/{name}/reset",
            post(handlers::branches::reset_branch),
        )
        .route(
            "/media-sets/{rid}/branches/{name}/merge",
            post(handlers::branches::merge_branch),
        )
        // ── Transactions ────────────────────────────────────────────
        .route(
            "/media-sets/{rid}/transactions",
            post(handlers::transactions::open_transaction)
                .get(handlers::transactions::list_transactions),
        )
        .route(
            "/transactions/{rid}/commit",
            post(handlers::transactions::commit_transaction),
        )
        .route(
            "/transactions/{rid}/abort",
            post(handlers::transactions::abort_transaction),
        )
        // ── Virtual media sets ─────────────────────────────────────
        .route(
            "/media-sets/{rid}/virtual-items",
            post(handlers::items::register_virtual_item),
        )
        // ── Items / presigned URLs ─────────────────────────────────
        .route(
            "/media-sets/{rid}/items/upload-url",
            post(handlers::items::presigned_upload),
        )
        .route(
            "/media-sets/{rid}/items",
            get(handlers::items::list_items),
        )
        .route("/items/{rid}", get(handlers::items::get_item))
        .route(
            "/items/{rid}/download-url",
            get(handlers::items::presigned_download),
        )
        .route("/items/{rid}", delete(handlers::items::delete_item))
        .layer(from_fn_with_state(
            (*state.jwt_config).clone(),
            auth_middleware::layer::auth_layer,
        ));

    let public = Router::new()
        .route("/healthz", get(handlers::health::healthz))
        .route("/health", get(handlers::health::healthz))
        .route("/metrics", get(handlers::health::metrics));

    Router::new()
        .merge(api)
        .merge(public)
        .layer(TraceLayer::new_for_http())
        .layer(audit_trail::middleware::audit_layer())
        .with_state(state)
}
