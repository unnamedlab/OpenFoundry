//! P5 — Foundry Marketplace dataset products.
//!
//! Two endpoints:
//!
//!   POST /v1/products/from-dataset/{rid} — publish a dataset as a
//!   versioned Marketplace product. Builds the `DatasetProductManifest`
//!   from the request fields gated by `include_*` toggles.
//!
//!   POST /v1/products/{id}/install — replay the manifest in another
//!   project. The runner is intentionally synchronous + minimal: it
//!   inserts the install row in `pending` and a real-runtime hook
//!   would later flip it to `ready` after copying bytes / wiring
//!   schedules. For unit-tests we surface enough of the manifest to
//!   prove the round-trip stays lossless.

use axum::{
    Json,
    extract::{Path, State},
};
use chrono::Utc;
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, internal_error, not_found},
    models::dataset_product::{
        CreateDatasetProductRequest, DatasetProduct, DatasetProductBootstrap,
        DatasetProductInstall, DatasetProductInstallRow, DatasetProductManifest, DatasetProductRow,
        InstallDatasetProductRequest,
    },
};

const ALLOWED_BOOTSTRAP_MODES: [&str; 2] = ["schema-only", "with-snapshot"];

pub async fn create_from_dataset(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Json(req): Json<CreateDatasetProductRequest>,
) -> ServiceResult<DatasetProduct> {
    if req.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    if rid.trim().is_empty() {
        return Err(bad_request("dataset RID is required"));
    }
    if !ALLOWED_BOOTSTRAP_MODES.contains(&req.bootstrap_mode.as_str()) {
        return Err(bad_request(
            "bootstrap_mode must be 'schema-only' or 'with-snapshot'",
        ));
    }

    // Build the manifest. Each include_* toggle gates one fragment.
    let manifest = DatasetProductManifest {
        entity: "dataset".into(),
        version: req.version.clone(),
        schema: if req.include_schema {
            req.schema.clone()
        } else {
            None
        },
        retention: if req.include_retention {
            req.retention.clone()
        } else {
            Vec::new()
        },
        branching_policy: if req.include_branches {
            req.branching_policy.clone()
        } else {
            None
        },
        schedules: if req.include_schedules {
            req.schedules.clone()
        } else {
            Vec::new()
        },
        bootstrap: DatasetProductBootstrap {
            mode: req.bootstrap_mode.clone(),
        },
    };

    let manifest_value =
        serde_json::to_value(&manifest).map_err(|e| internal_error(e.to_string()))?;
    let id = Uuid::now_v7();

    let row = sqlx::query_as::<_, DatasetProductRow>(
        r#"INSERT INTO marketplace_dataset_products (
                id, name, source_dataset_rid, entity_type, version,
                project_id, published_by,
                export_includes_data, include_schema, include_branches,
                include_retention, include_schedules,
                manifest, bootstrap_mode, published_at, created_at
           ) VALUES (
                $1, $2, $3, 'dataset', $4,
                $5, $6,
                $7, $8, $9, $10, $11,
                $12::jsonb, $13, NOW(), NOW()
           )
           RETURNING id, name, source_dataset_rid, entity_type, version,
                     project_id, published_by,
                     export_includes_data, include_schema, include_branches,
                     include_retention, include_schedules,
                     manifest, bootstrap_mode, published_at, created_at"#,
    )
    .bind(id)
    .bind(&req.name)
    .bind(&rid)
    .bind(&req.version)
    .bind(req.project_id)
    .bind(req.published_by)
    .bind(req.export_includes_data)
    .bind(req.include_schema)
    .bind(req.include_branches)
    .bind(req.include_retention)
    .bind(req.include_schedules)
    .bind(manifest_value)
    .bind(&req.bootstrap_mode)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(DatasetProduct::try_from(row).map_err(internal_error)?))
}

pub async fn install_dataset_product(
    State(state): State<AppState>,
    Path(product_id): Path<Uuid>,
    Json(req): Json<InstallDatasetProductRequest>,
) -> ServiceResult<DatasetProductInstall> {
    if req.target_dataset_rid.trim().is_empty() {
        return Err(bad_request("target_dataset_rid is required"));
    }

    let product_row = sqlx::query_as::<_, DatasetProductRow>(
        r#"SELECT id, name, source_dataset_rid, entity_type, version,
                  project_id, published_by,
                  export_includes_data, include_schema, include_branches,
                  include_retention, include_schedules,
                  manifest, bootstrap_mode, published_at, created_at
             FROM marketplace_dataset_products
            WHERE id = $1"#,
    )
    .bind(product_id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("dataset product not found"))?;

    let bootstrap_mode = req
        .bootstrap_mode
        .clone()
        .unwrap_or_else(|| product_row.bootstrap_mode.clone());
    if !ALLOWED_BOOTSTRAP_MODES.contains(&bootstrap_mode.as_str()) {
        return Err(bad_request(
            "bootstrap_mode must be 'schema-only' or 'with-snapshot'",
        ));
    }

    // The actual replay (catalog row + schema upsert + optional
    // bytes copy via P3 backing-fs) is async work for the runner. For
    // now we record an install row so callers can poll status.
    // Manifest bytes are returned to the caller via `details` so a
    // round-trip test can compare snapshot vs install without a join.
    let details = serde_json::json!({
        "manifest_replay": product_row.manifest,
        "source_dataset_rid": product_row.source_dataset_rid,
        "version": product_row.version,
    });

    let install_row = sqlx::query_as::<_, DatasetProductInstallRow>(
        r#"INSERT INTO marketplace_dataset_product_installs (
                id, product_id, target_project_id, target_dataset_rid,
                bootstrap_mode, status, details, installed_by,
                created_at, completed_at
           ) VALUES (
                $1, $2, $3, $4, $5, 'pending', $6::jsonb, $7, NOW(), NULL
           )
           RETURNING id, product_id, target_project_id, target_dataset_rid,
                     bootstrap_mode, status, details, installed_by,
                     created_at, completed_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(product_id)
    .bind(req.target_project_id)
    .bind(&req.target_dataset_rid)
    .bind(&bootstrap_mode)
    .bind(details)
    .bind(req.installed_by)
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(DatasetProductInstall::from(install_row)))
}

pub async fn get_dataset_product(
    State(state): State<AppState>,
    Path(product_id): Path<Uuid>,
) -> ServiceResult<DatasetProduct> {
    let row = sqlx::query_as::<_, DatasetProductRow>(
        r#"SELECT id, name, source_dataset_rid, entity_type, version,
                  project_id, published_by,
                  export_includes_data, include_schema, include_branches,
                  include_retention, include_schedules,
                  manifest, bootstrap_mode, published_at, created_at
             FROM marketplace_dataset_products
            WHERE id = $1"#,
    )
    .bind(product_id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("dataset product not found"))?;

    Ok(Json(DatasetProduct::try_from(row).map_err(internal_error)?))
}

/// Sentinel use of UTC so the import doesn't drift to dead-code in
/// future revisions of this module.
#[allow(dead_code)]
fn _now() -> chrono::DateTime<Utc> {
    Utc::now()
}
