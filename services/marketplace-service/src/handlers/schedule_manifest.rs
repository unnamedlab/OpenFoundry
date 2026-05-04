//! Per-product schedule manifests (P3). Mirrors the Foundry doc
//! § "Add schedule to a Marketplace product".

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use uuid::Uuid;

use crate::AppState;
use crate::models::schedule_manifest::{AddScheduleManifestRequest, RidMapping, ScheduleManifest};

#[derive(Debug, Deserialize)]
pub struct InstallSchedulesBody {
    /// Active product version that the install is materialising.
    pub product_version_id: Uuid,
    /// Pipeline / dataset RID rewrites the install pass discovered
    /// when planning the rest of the product. Applied to every
    /// manifest before posting it to pipeline-schedule-service.
    #[serde(default)]
    pub rid_mapping: RidMapping,
    /// Names of manifests the operator opted to activate at install
    /// time. An empty list means "every manifest in the version".
    #[serde(default)]
    pub activate_manifests: Vec<String>,
}

/// `POST /v1/products/{product_rid}/schedules` — add a manifest.
pub async fn add_schedule_manifest(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path(_product_rid): Path<String>,
    Json(body): Json<AddScheduleManifestRequest>,
) -> impl IntoResponse {
    let manifest_json = match serde_json::to_value(&body.manifest) {
        Ok(v) => v,
        Err(e) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(json!({ "error": e.to_string() })),
            )
                .into_response();
        }
    };
    let result = sqlx::query(
        r#"INSERT INTO marketplace_schedule_manifests
                (id, product_version_id, name, manifest_json)
            VALUES ($1, $2, $3, $4)
            ON CONFLICT (product_version_id, name)
            DO UPDATE SET manifest_json = EXCLUDED.manifest_json
            RETURNING id"#,
    )
    .bind(Uuid::now_v7())
    .bind(body.product_version_id)
    .bind(&body.manifest.name)
    .bind(manifest_json)
    .fetch_one(&state.db)
    .await;
    match result {
        Ok(row) => {
            let id: Uuid = row.try_get("id").unwrap_or_else(|_| Uuid::nil());
            (
                StatusCode::CREATED,
                Json(json!({
                    "id": id,
                    "product_version_id": body.product_version_id,
                    "name": body.manifest.name,
                })),
            )
                .into_response()
        }
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": e.to_string() })),
        )
            .into_response(),
    }
}

/// `POST /v1/products/{product_rid}/install:schedules` — install pass
/// helper that materialises every selected manifest into a local
/// schedule. Returns the list of manifests transformed by the rid
/// mapping. The actual `pipeline-schedule-service.POST /v1/schedules`
/// call is the install pass's job (kept here as a pure projection so
/// tests can drive it without networking).
pub async fn materialise_install_schedules(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path(_product_rid): Path<String>,
    Json(body): Json<InstallSchedulesBody>,
) -> impl IntoResponse {
    let rows = match sqlx::query(
        r#"SELECT name, manifest_json FROM marketplace_schedule_manifests
            WHERE product_version_id = $1
            ORDER BY name ASC"#,
    )
    .bind(body.product_version_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(r) => r,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": e.to_string() })),
            )
                .into_response();
        }
    };

    let mut materialised = Vec::with_capacity(rows.len());
    for row in rows {
        let name: String = match row.try_get("name") {
            Ok(n) => n,
            Err(_) => continue,
        };
        if !body.activate_manifests.is_empty() && !body.activate_manifests.contains(&name) {
            continue;
        }
        let manifest_json: serde_json::Value = match row.try_get("manifest_json") {
            Ok(v) => v,
            Err(_) => continue,
        };
        let mut manifest: ScheduleManifest = match serde_json::from_value(manifest_json) {
            Ok(m) => m,
            Err(_) => continue,
        };
        body.rid_mapping.rewrite(&mut manifest.target);
        body.rid_mapping.rewrite(&mut manifest.trigger);
        materialised.push(json!({
            "name": manifest.name,
            "trigger": manifest.trigger,
            "target": manifest.target,
            "scope_kind": manifest.scope_kind,
            "defaults": manifest.defaults,
        }));
    }

    Json(json!({
        "product_version_id": body.product_version_id,
        "materialised": materialised,
    }))
    .into_response()
}
