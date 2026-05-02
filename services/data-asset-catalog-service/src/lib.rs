//! `data-asset-catalog-service` — biblioteca compartida.
//!
//! Expone:
//! * [`AppState`]: estado compartido por handlers (PgPool, `StorageBackend`,
//!   `LakehousePrefixes` y URL del servicio de calidad de datos).
//! * [`build_router`]: ensambla el router Axum con autenticación JWT, audit
//!   logging básico (`tracing`) y todos los endpoints de los handlers.
//!
//! El binario en `src/main.rs` lee la configuración, abre el pool de
//! Postgres, ejecuta migraciones, instancia el backend de objetos y
//! arranca el listener TCP.

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use sqlx::PgPool;
use storage_abstraction::StorageBackend;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;
pub mod security;

use crate::config::LakehousePrefixes;
use crate::domain::markings::MarkingResolver;

/// Estado compartido por todos los handlers HTTP.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub storage: Arc<dyn StorageBackend>,
    pub lakehouse_prefixes: LakehousePrefixes,
    pub dataset_quality_service_url: String,
    pub jwt_config: Arc<JwtConfig>,
    pub http_client: reqwest::Client,
    /// Optional T3.2 marking resolver. When present, dataset reads
    /// surface effective (inherited) markings; when absent the legacy
    /// tag-derived path is used.
    #[doc(hidden)]
    pub marking_resolver: Option<Arc<MarkingResolver>>,
}

/// Construye el router HTTP completo del servicio.
pub fn build_router(state: AppState) -> Router {
    use axum::middleware::from_fn_with_state;
    use axum::routing::{get, post};
    use tower_http::trace::TraceLayer;

    // ── API protegida por JWT ─────────────────────────────────────────────
    let api = Router::new()
        // Catálogo / facets
        .route(
            "/v1/catalog/facets",
            get(handlers::catalog::get_catalog_facets),
        )
        // CRUD de datasets (T0.2 monta la firma `/v1/datasets/{rid}`).
        .route(
            "/v1/datasets",
            get(handlers::crud::list_datasets).post(handlers::crud::create_dataset),
        )
        .route(
            "/v1/datasets/{rid}",
            get(handlers::crud::get_dataset)
                .patch(handlers::crud::update_dataset)
                .delete(handlers::crud::delete_dataset),
        )
        // Preview / schema
        .route(
            "/v1/datasets/{rid}/preview",
            get(handlers::preview::preview_data),
        )
        .route(
            "/v1/datasets/{rid}/schema",
            get(handlers::preview::get_schema),
        )
        // T6.3 — schema validation against the current view's files.
        .route(
            "/v1/datasets/{rid}/schema:validate",
            post(handlers::schema_validate::validate_schema),
        )
        // Upload (multipart)
        .route(
            "/v1/datasets/{rid}/upload",
            post(handlers::upload::upload_data),
        )
        // Files (export listing)
        .route("/v1/datasets/{rid}/files", get(handlers::export::list_files))
        // Views — listado, creación, lectura puntual, preview y refresh.
        // `:refresh` se enruta como segmento único y se despacha en el
        // wrapper `view_action` (Axum 0.8 no soporta sufijo literal en
        // captura).
        .route(
            "/v1/datasets/{rid}/views",
            get(handlers::views::list_views).post(handlers::views::create_view),
        )
        .route(
            "/v1/datasets/{rid}/views/{view_or_action}",
            get(handlers::views::get_view).post(view_action_dispatch),
        )
        .route(
            "/v1/datasets/{rid}/views/{view_id}/preview",
            get(handlers::views::preview_view),
        )
        // Internal API (consumida por servicios vecinos detrás de la malla
        // de servicio; puede dejar de exigir JWT en una futura revisión).
        .route(
            "/internal/datasets/{rid}/metadata",
            get(handlers::internal::get_dataset_metadata),
        )
        .layer(from_fn_with_state(
            (*state.jwt_config).clone(),
            auth_middleware::layer::auth_layer,
        ));

    let public = Router::new()
        .route("/healthz", get(healthz))
        .route("/health", get(healthz))
        .route("/metrics", get(metrics_endpoint));

    // Ensure the `dataset_*` metric families are registered before the
    // first scrape so smoke tests always see the prefix in /metrics.
    crate::metrics::init();

    Router::new()
        .merge(api)
        .merge(public)
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}

async fn healthz() -> &'static str {
    "ok"
}

async fn metrics_endpoint() -> axum::response::Response {
    use axum::http::{StatusCode, header::CONTENT_TYPE};
    use axum::response::IntoResponse;
    use prometheus::{Encoder, TextEncoder};

    let metric_families = prometheus::gather();
    let mut buffer = Vec::new();
    let encoder = TextEncoder::new();
    if encoder.encode(&metric_families, &mut buffer).is_err() {
        return (StatusCode::INTERNAL_SERVER_ERROR, "encode failure").into_response();
    }
    (
        StatusCode::OK,
        [(CONTENT_TYPE, encoder.format_type())],
        buffer,
    )
        .into_response()
}

/// Despacha `POST /v1/datasets/{rid}/views/{view_or_action}` segmentando
/// el sufijo `:refresh` exigido por la API Foundry-style.
async fn view_action_dispatch(
    state: axum::extract::State<AppState>,
    auth: auth_middleware::layer::AuthUser,
    axum::extract::Path((rid, view_or_action)): axum::extract::Path<(uuid::Uuid, String)>,
) -> axum::response::Response {
    use axum::response::IntoResponse;

    let (view_segment, action) = match view_or_action.split_once(':') {
        Some(parts) => parts,
        None => {
            return (
                axum::http::StatusCode::METHOD_NOT_ALLOWED,
                axum::Json(serde_json::json!({
                    "error": "POST on /views/{id} requires a ':refresh' action suffix",
                })),
            )
                .into_response();
        }
    };
    if action != "refresh" {
        return (
            axum::http::StatusCode::BAD_REQUEST,
            axum::Json(serde_json::json!({
                "error": "unsupported view action; only ':refresh' is supported",
            })),
        )
            .into_response();
    }
    let view_id = match uuid::Uuid::parse_str(view_segment) {
        Ok(id) => id,
        Err(_) => {
            return (
                axum::http::StatusCode::BAD_REQUEST,
                axum::Json(serde_json::json!({ "error": "view id is not a valid UUID" })),
            )
                .into_response();
        }
    };
    let _ = auth; // auth_layer ya validó el token; los handlers downstream re-extraen claims.
    handlers::views::refresh_view(state, axum::extract::Path((rid, view_id)))
        .await
        .into_response()
}
