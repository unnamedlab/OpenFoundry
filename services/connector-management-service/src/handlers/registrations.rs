//! Virtual table registration handlers.
//!
//! Mirrors the Foundry "Virtual tables" workflow described in
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//! Core concepts/Virtual tables.md`:
//!
//!   * **Discovery** — surface the catalog of objects the source exposes
//!     (tables, files, topics, …) so the user can pick which ones to register.
//!   * **Bulk registration** — register many discovered selectors at once,
//!     setting `registration_mode` (`sync` vs `zero_copy`), `auto_sync` and
//!     `update_detection` per item. Equivalent to the bulk-register pane in
//!     the Foundry source view.
//!   * **Auto-registration (one-shot)** — registers every discovered selector
//!     (or a filtered subset) under the chosen defaults. The recurring
//!     variant is implemented in [`crate::domain::auto_registration`] and is
//!     opt-in via `connections.config.auto_registration.enabled`.
//!
//! The handlers below wire the previously-dead helpers in
//! [`crate::domain::discovery`] into the HTTP surface.

use auth_middleware::claims::Claims;
use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    domain::discovery,
    models::{
        connection::Connection,
        registration::{
            AutoRegisterRequest, BulkRegisterRequest, ConnectionRegistration,
            VirtualTableQueryRequest,
        },
    },
};

/// POST /api/v1/data-connection/sources/{id}/registrations/discover
///
/// Returns the catalog of selectors the source exposes. Wraps
/// [`discovery::discover_sources`].
pub async fn discover(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, connection_id).await {
        Ok(connection) => connection,
        Err(response) => return response,
    };
    if !can_manage(&claims, &connection) {
        return StatusCode::FORBIDDEN.into_response();
    }
    match discovery::discover_sources(&state, &connection).await {
        Ok(sources) => Json(json!({ "sources": sources })).into_response(),
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

/// GET /api/v1/data-connection/sources/{id}/registrations
pub async fn list_registrations(
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, ConnectionRegistration>(
        "SELECT * FROM connection_registrations WHERE connection_id = $1
         ORDER BY created_at DESC",
    )
    .bind(connection_id)
    .fetch_all(&state.db)
    .await;
    match rows {
        Ok(items) => Json(json!({ "registrations": items })).into_response(),
        Err(error) => {
            tracing::error!("list registrations failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

/// POST /api/v1/data-connection/sources/{id}/registrations/bulk
///
/// Foundry "bulk register" equivalent. Each item names a selector that the
/// user already chose from the discovery panel.
pub async fn bulk_register(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
    Json(body): Json<BulkRegisterRequest>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, connection_id).await {
        Ok(connection) => connection,
        Err(response) => return response,
    };
    if !can_manage(&claims, &connection) {
        return StatusCode::FORBIDDEN.into_response();
    }
    if body.registrations.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({ "error": "registrations array is empty" })),
        )
            .into_response();
    }

    // Discovery is required to confirm each selector exists and to capture
    // its source_kind/metadata. We tolerate sources that fail discovery (e.g.
    // agent-bridged ones offline) by falling back to the user-supplied data.
    let discovered = discovery::discover_sources(&state, &connection)
        .await
        .unwrap_or_default();

    let mut created = Vec::with_capacity(body.registrations.len());
    let mut errors = Vec::new();
    for item in body.registrations {
        let mode = match discovery::normalize_registration_mode(item.registration_mode.as_deref())
        {
            Ok(mode) => mode,
            Err(error) => {
                errors.push(json!({ "selector": item.selector, "error": error }));
                continue;
            }
        };
        let auto_sync = item.auto_sync.unwrap_or(false);
        let update_detection = item.update_detection.unwrap_or(true);

        let discovered_match = discovered.iter().find(|d| d.selector == item.selector);
        let template = match discovered_match {
            Some(found) => found.clone(),
            None => crate::models::registration::DiscoveredSource {
                selector: item.selector.clone(),
                display_name: item
                    .display_name
                    .clone()
                    .unwrap_or_else(|| item.selector.clone()),
                source_kind: item
                    .source_kind
                    .clone()
                    .unwrap_or_else(|| connection.connector_type.clone()),
                supports_sync: true,
                supports_zero_copy: false,
                source_signature: None,
                metadata: serde_json::Value::Null,
            },
        };

        match discovery::upsert_registration(
            &state,
            connection_id,
            &template,
            mode,
            auto_sync,
            update_detection,
            item.target_dataset_id,
            item.metadata,
        )
        .await
        {
            Ok(reg) => created.push(reg),
            Err(error) => {
                errors.push(json!({ "selector": item.selector, "error": error }));
            }
        }
    }

    Json(json!({
        "created": created,
        "errors": errors,
    }))
    .into_response()
}

/// POST /api/v1/data-connection/sources/{id}/registrations/auto
///
/// Foundry "automatic registration" one-shot equivalent: discovers every
/// selector (optionally filtered) and registers them with the supplied
/// defaults. The recurring variant is the scheduler in
/// [`crate::domain::auto_registration`].
pub async fn auto_register(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
    Json(body): Json<AutoRegisterRequest>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, connection_id).await {
        Ok(connection) => connection,
        Err(response) => return response,
    };
    if !can_manage(&claims, &connection) {
        return StatusCode::FORBIDDEN.into_response();
    }
    let mode = match discovery::normalize_registration_mode(body.registration_mode.as_deref()) {
        Ok(mode) => mode,
        Err(error) => {
            return (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response();
        }
    };

    let discovered = match discovery::discover_sources(&state, &connection).await {
        Ok(items) => items,
        Err(error) => {
            return (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response();
        }
    };

    let auto_sync = body.auto_sync.unwrap_or(false);
    let update_detection = body.update_detection.unwrap_or(true);
    let selected = discovery::select_sources(&discovered, &body);

    let mut created = Vec::with_capacity(selected.len());
    let mut errors = Vec::new();
    for source in selected {
        match discovery::upsert_registration(
            &state,
            connection_id,
            source,
            mode,
            auto_sync,
            update_detection,
            body.default_target_dataset_id,
            json!({ "origin": "auto_register" }),
        )
        .await
        {
            Ok(reg) => created.push(reg),
            Err(error) => {
                errors.push(json!({ "selector": source.selector, "error": error }));
            }
        }
    }

    Json(json!({
        "discovered_count": discovered.len(),
        "created": created,
        "errors": errors,
    }))
    .into_response()
}

/// POST /api/v1/data-connection/sources/{id}/registrations/{registration_id}/query
///
/// **Zero-copy read** of a registered virtual table. Resolves the registration
/// to its source `Connection` and dispatches into
/// [`discovery::query_virtual_table`], which delegates to the matching
/// connector's in-place reader (Postgres `SELECT … LIMIT n`, S3 Parquet
/// listing, Iceberg snapshot scan, …). The response is a `VirtualTableQueryResponse`
/// — rows are returned to the caller verbatim, never persisted in Foundry
/// storage. This is the primitive backing both:
///
///   * Foundry-side compute (Contour, Pipeline Builder, dataset preview)
///   * External engines via the Iceberg REST catalog at `/iceberg/v1/*`
///     (see [`crate::handlers::iceberg_catalog`]).
#[derive(Debug, serde::Deserialize, Default)]
pub struct QueryRegistrationBody {
    #[serde(default)]
    pub limit: Option<usize>,
}

pub async fn query_registration(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((connection_id, registration_id)): Path<(Uuid, Uuid)>,
    body: Option<Json<QueryRegistrationBody>>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, connection_id).await {
        Ok(connection) => connection,
        Err(response) => return response,
    };
    if !can_manage(&claims, &connection) {
        return StatusCode::FORBIDDEN.into_response();
    }
    let registration = match sqlx::query_as::<_, ConnectionRegistration>(
        "SELECT * FROM connection_registrations WHERE id = $1 AND connection_id = $2",
    )
    .bind(registration_id)
    .bind(connection_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(reg)) => reg,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("registration lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let limit = body.and_then(|Json(b)| b.limit);
    let request = VirtualTableQueryRequest {
        selector: registration.selector.clone(),
        limit,
    };
    match discovery::query_virtual_table(&state, &connection, &request).await {
        Ok(response) => Json(response).into_response(),
        Err(error) => {
            tracing::warn!(
                connection_id = %connection_id,
                registration_id = %registration_id,
                "virtual table query failed: {error}"
            );
            (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response()
        }
    }
}

/// DELETE /api/v1/data-connection/sources/{id}/registrations/{registration_id}
pub async fn delete_registration(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((connection_id, registration_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    let connection = match load_connection(&state, connection_id).await {
        Ok(connection) => connection,
        Err(response) => return response,
    };
    if !can_manage(&claims, &connection) {
        return StatusCode::FORBIDDEN.into_response();
    }
    let result = sqlx::query(
        "DELETE FROM connection_registrations WHERE id = $1 AND connection_id = $2",
    )
    .bind(registration_id)
    .bind(connection_id)
    .execute(&state.db)
    .await;
    match result {
        Ok(res) if res.rows_affected() == 0 => StatusCode::NOT_FOUND.into_response(),
        Ok(_) => StatusCode::NO_CONTENT.into_response(),
        Err(error) => {
            tracing::error!("delete registration failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

fn can_manage(claims: &Claims, connection: &Connection) -> bool {
    claims.has_role("admin")
        || claims.has_permission("connections", "write")
        || claims.sub == connection.owner_id
}

async fn load_connection(
    state: &AppState,
    connection_id: Uuid,
) -> Result<Connection, axum::response::Response> {
    match sqlx::query_as::<_, Connection>("SELECT * FROM connections WHERE id = $1")
        .bind(connection_id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(connection)) => Ok(connection),
        Ok(None) => Err(StatusCode::NOT_FOUND.into_response()),
        Err(error) => {
            tracing::error!("connection lookup failed: {error}");
            Err(StatusCode::INTERNAL_SERVER_ERROR.into_response())
        }
    }
}
