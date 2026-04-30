use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Serialize;
use uuid::Uuid;

use crate::{
    AppState,
    models::{
        connection::Connection,
        connector_profile::{
            ConnectionCapabilityResponse, ConnectorContractCatalog, ConnectorContractProfile,
            certification_summary, connector_profile, connector_profiles,
        },
    },
};

/// Subset of the connector contract that the Data Connection gallery
/// renders. Mirrors `ConnectorCatalogEntry` in
/// `apps/web/src/lib/api/data-connection.ts` so the frontend can typecheck
/// against the backend without an extra adapter layer.
#[derive(Debug, Serialize)]
pub struct GalleryConnector {
    pub r#type: String,
    pub name: String,
    pub description: String,
    pub capabilities: Vec<String>,
    pub workers: Vec<String>,
    pub available: bool,
}

#[derive(Debug, Serialize)]
pub struct GalleryCatalog {
    pub connectors: Vec<GalleryConnector>,
}

/// Connector types the MVP can actually create end-to-end. Anything outside
/// this list is advertised as `available: false` (gallery shows it but the
/// "+ New source" flow is disabled). Keep in sync with the README MVP scope
/// — do not flip a connector to `true` until its validate_config + worker
/// path is shipped.
const AVAILABLE_TYPES: &[&str] = &[
    "postgresql",
    "mysql",
    "s3",
    "parquet",
    "rest_api",
    "csv",
    "json",
];

/// GET /api/v1/data-connection/catalog — gallery shape for the UI.
pub async fn get_connector_catalog() -> impl IntoResponse {
    let connectors = connector_profiles()
        .into_iter()
        .map(to_gallery_connector)
        .collect();
    Json(GalleryCatalog { connectors }).into_response()
}

/// GET /api/v1/data-connection/catalog/contracts — full contract registry.
/// Kept for tooling that needs the rich auth/sync/observability matrix.
pub async fn get_connector_contracts() -> impl IntoResponse {
    Json(ConnectorContractCatalog {
        connectors: connector_profiles(),
        certification_summary: certification_summary(),
    })
    .into_response()
}

fn to_gallery_connector(profile: ConnectorContractProfile) -> GalleryConnector {
    let mut capabilities: Vec<String> = profile
        .sync
        .modes
        .iter()
        .map(|m| match m.as_str() {
            "batch" => "batch_sync".to_string(),
            "incremental" => "batch_sync".to_string(),
            "streaming" => "streaming_sync".to_string(),
            "zero_copy" => "virtual_table".to_string(),
            other => other.to_string(),
        })
        .collect();
    if profile.sync.supports_cdc {
        capabilities.push("cdc_sync".to_string());
    }
    if profile.testing.supports_discovery {
        capabilities.push("exploration".to_string());
    }
    capabilities.sort();
    capabilities.dedup();

    let mut workers = vec!["foundry".to_string()];
    if profile.auth.supports_private_network_agent {
        workers.push("agent".to_string());
    }

    let description = profile
        .notes
        .first()
        .cloned()
        .unwrap_or_else(|| profile.template_family.clone());

    let available = AVAILABLE_TYPES.contains(&profile.connector_type.as_str());

    GalleryConnector {
        r#type: profile.connector_type,
        name: profile.display_name,
        description,
        capabilities,
        workers,
        available,
    }
}

pub async fn get_connection_capabilities(
    State(state): State<AppState>,
    Path(connection_id): Path<Uuid>,
) -> impl IntoResponse {
    let connection =
        match sqlx::query_as::<_, Connection>("SELECT * FROM connections WHERE id = $1")
            .bind(connection_id)
            .fetch_optional(&state.db)
            .await
        {
            Ok(Some(connection)) => connection,
            Ok(None) => return StatusCode::NOT_FOUND.into_response(),
            Err(error) => {
                tracing::error!("connection capability lookup failed: {error}");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
        };

    let Some(contract) = connector_profile(&connection.connector_type) else {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({
                "error": format!("no connector contract available for {}", connection.connector_type)
            })),
        )
            .into_response();
    };

    Json(ConnectionCapabilityResponse {
        connection_id: connection.id,
        connector_type: connection.connector_type,
        status: connection.status,
        contract,
    })
    .into_response()
}

