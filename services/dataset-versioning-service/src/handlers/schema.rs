//! T6.x — view-scoped schema endpoints.
//!
//! Foundry stores schemas as metadata on a *dataset view* (see
//! `Datasets.md` § "Schemas"). Each commit produces a new view, so a
//! schema-changing transaction lands in a fresh row of
//! `dataset_view_schemas`.
//!
//! Routes:
//! * `POST /v1/datasets/{rid}/views/{view_id}/schema` — upsert. Idempotent
//!   by content hash: if the incoming payload hashes to the same value
//!   already stored, returns 200 OK with `{ "unchanged": true }`. Otherwise
//!   200 OK with the new payload (or 201 Created on first insert).
//! * `GET  /v1/datasets/{rid}/views/{view_id}/schema` — read.
//! * `GET  /v1/datasets/{rid}/schema` — legacy compat, returns the schema
//!   of the *current* view (resolved through the active branch).

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::models::schema::{DatasetSchema, FileFormat, validate_schema};

// ─────────────────────────────────────────────────────────────────────────────
// Wire types
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Serialize)]
pub struct SchemaResponse {
    pub view_id: Uuid,
    pub dataset_id: Uuid,
    pub branch: Option<String>,
    pub schema: DatasetSchema,
    pub content_hash: String,
    pub created_at: DateTime<Utc>,
    /// True when a POST didn't change anything because the payload hashed
    /// to the value already stored.
    #[serde(default, skip_serializing_if = "is_false")]
    pub unchanged: bool,
}

fn is_false(b: &bool) -> bool {
    !*b
}

#[derive(Debug, Deserialize)]
pub struct PutSchemaBody {
    /// The new schema. Must validate before being persisted.
    pub schema: DatasetSchema,
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "dataset-versioning-service: schema handler error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}

fn bad_request(msg: impl Into<String>) -> (StatusCode, Json<Value>) {
    (
        StatusCode::BAD_REQUEST,
        Json(json!({ "error": msg.into() })),
    )
}

fn not_found(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::NOT_FOUND, Json(json!({ "error": msg })))
}

async fn resolve_dataset_id(
    state: &AppState,
    rid: &str,
) -> Result<Uuid, (StatusCode, Json<Value>)> {
    if let Ok(uuid) = Uuid::parse_str(rid) {
        return Ok(uuid);
    }
    let row = sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?;
    row.ok_or_else(|| not_found("dataset not found"))
}

/// Returns the current view's id for a dataset on its active branch.
/// Used by the legacy `GET /v1/datasets/{rid}/schema` route.
async fn current_view_id(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Option<(Uuid, String)>, (StatusCode, Json<Value>)> {
    let row = sqlx::query_as::<_, (Option<Uuid>, String)>(
        r#"SELECT v.id, b.name
             FROM datasets d
             LEFT JOIN dataset_branches b
                    ON b.dataset_id = d.id
                   AND b.name = d.active_branch
                   AND b.deleted_at IS NULL
             LEFT JOIN dataset_views v
                    ON v.dataset_id = d.id
                   AND v.branch_id = b.id
            WHERE d.id = $1
            ORDER BY v.computed_at DESC NULLS LAST
            LIMIT 1"#,
    )
    .bind(dataset_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?;

    Ok(row.and_then(|(view, branch)| view.map(|id| (id, branch))))
}

#[derive(sqlx::FromRow)]
struct SchemaRow {
    view_id: Uuid,
    schema_json: Value,
    file_format: String,
    custom_metadata: Option<Value>,
    content_hash: String,
    created_at: DateTime<Utc>,
}

async fn load_schema_row(
    state: &AppState,
    view_id: Uuid,
) -> Result<Option<SchemaRow>, (StatusCode, Json<Value>)> {
    sqlx::query_as::<_, SchemaRow>(
        r#"SELECT view_id, schema_json, file_format, custom_metadata, content_hash, created_at
             FROM dataset_view_schemas
            WHERE view_id = $1"#,
    )
    .bind(view_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)
}

fn build_response(
    row: SchemaRow,
    dataset_id: Uuid,
    branch: Option<String>,
    unchanged: bool,
) -> Result<SchemaResponse, (StatusCode, Json<Value>)> {
    // Reconstruct the typed schema. The trigger may have stored a
    // schema_json that omits `file_format`; we patch it from the
    // dedicated column for callers that read straight from
    // dataset_view_schemas.
    let mut schema: DatasetSchema = serde_json::from_value(row.schema_json).map_err(internal)?;
    schema.file_format = parse_file_format(&row.file_format);
    if let Some(meta) = row.custom_metadata {
        if !meta.is_null() {
            schema.custom_metadata = serde_json::from_value(meta).map_err(internal)?;
        }
    }
    Ok(SchemaResponse {
        view_id: row.view_id,
        dataset_id,
        branch,
        schema,
        content_hash: row.content_hash,
        created_at: row.created_at,
        unchanged,
    })
}

fn parse_file_format(raw: &str) -> FileFormat {
    match raw.to_ascii_uppercase().as_str() {
        "AVRO" => FileFormat::Avro,
        "TEXT" => FileFormat::Text,
        _ => FileFormat::Parquet,
    }
}

async fn assert_view_belongs_to_dataset(
    state: &AppState,
    dataset_id: Uuid,
    view_id: Uuid,
) -> Result<Option<String>, (StatusCode, Json<Value>)> {
    let row = sqlx::query_as::<_, (Uuid, String)>(
        r#"SELECT v.dataset_id, b.name
             FROM dataset_views v
             JOIN dataset_branches b ON b.id = v.branch_id
            WHERE v.id = $1"#,
    )
    .bind(view_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?;

    match row {
        Some((ds, branch)) if ds == dataset_id => Ok(Some(branch)),
        Some(_) => Err(bad_request("view_id does not belong to this dataset")),
        None => Ok(None),
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

/// `GET /v1/datasets/{rid}/views/{view_id}/schema`.
pub async fn get_view_schema(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, view_id_str)): Path<(String, String)>,
) -> Result<Json<SchemaResponse>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let view_id =
        Uuid::parse_str(&view_id_str).map_err(|_| bad_request("view_id is not a valid UUID"))?;
    let branch = assert_view_belongs_to_dataset(&state, dataset_id, view_id).await?;
    let row = load_schema_row(&state, view_id)
        .await?
        .ok_or_else(|| not_found("schema not found for view"))?;
    Ok(Json(build_response(row, dataset_id, branch, false)?))
}

/// `POST /v1/datasets/{rid}/views/{view_id}/schema`.
///
/// Body: `{ "schema": <DatasetSchema> }`. Idempotent on content hash:
/// reposting the same schema returns 200 with `unchanged = true`.
pub async fn put_view_schema(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, view_id_str)): Path<(String, String)>,
    Json(body): Json<PutSchemaBody>,
) -> Result<(StatusCode, Json<SchemaResponse>), (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "schema.put")?;

    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let view_id =
        Uuid::parse_str(&view_id_str).map_err(|_| bad_request("view_id is not a valid UUID"))?;
    let branch = assert_view_belongs_to_dataset(&state, dataset_id, view_id)
        .await?
        .ok_or_else(|| not_found("view not found"))?;

    let schema = body.schema;
    validate_schema(&schema).map_err(|e| bad_request(e.to_string()))?;

    let content_hash = schema.content_hash();
    let schema_json = serde_json::to_value(&schema).map_err(internal)?;
    let custom_metadata = match &schema.custom_metadata {
        Some(meta) => Some(serde_json::to_value(meta).map_err(internal)?),
        None => None,
    };
    let file_format = schema.file_format.as_str();

    // Detect no-op early so we can return `unchanged = true` without
    // bumping `created_at` for callers that just want to re-confirm.
    let prior = load_schema_row(&state, view_id).await?;
    if let Some(existing) = &prior {
        if existing.content_hash == content_hash {
            return Ok((
                StatusCode::OK,
                Json(build_response(
                    SchemaRow {
                        view_id: existing.view_id,
                        schema_json: existing.schema_json.clone(),
                        file_format: existing.file_format.clone(),
                        custom_metadata: existing.custom_metadata.clone(),
                        content_hash: existing.content_hash.clone(),
                        created_at: existing.created_at,
                    },
                    dataset_id,
                    Some(branch),
                    true,
                )?),
            ));
        }
    }

    let row = sqlx::query_as::<_, SchemaRow>(
        r#"INSERT INTO dataset_view_schemas
                (view_id, schema_json, file_format, custom_metadata, content_hash, created_at)
            VALUES ($1, $2, $3, $4, $5, NOW())
            ON CONFLICT (view_id) DO UPDATE
                SET schema_json     = EXCLUDED.schema_json,
                    file_format     = EXCLUDED.file_format,
                    custom_metadata = EXCLUDED.custom_metadata,
                    content_hash    = EXCLUDED.content_hash,
                    created_at      = NOW()
            RETURNING view_id, schema_json, file_format, custom_metadata, content_hash, created_at"#,
    )
    .bind(view_id)
    .bind(&schema_json)
    .bind(file_format)
    .bind(custom_metadata.as_ref())
    .bind(&content_hash)
    .fetch_one(&state.db)
    .await
    .map_err(internal)?;

    let status = if prior.is_none() {
        StatusCode::CREATED
    } else {
        StatusCode::OK
    };

    crate::security::emit_audit(
        &user.0.sub,
        "schema.put",
        &rid,
        json!({
            "view_id": view_id,
            "content_hash": content_hash,
            "file_format": file_format,
            "field_count": schema.fields.len(),
        }),
    );

    Ok((
        status,
        Json(build_response(row, dataset_id, Some(branch), false)?),
    ))
}

/// Legacy compat: `GET /v1/datasets/{rid}/schema`. Returns the schema
/// of the dataset's *current* view (resolved through the active branch).
pub async fn get_current_schema(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<SchemaResponse>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let (view_id, branch) = current_view_id(&state, dataset_id)
        .await?
        .ok_or_else(|| not_found("no current view for dataset"))?;
    let row = load_schema_row(&state, view_id)
        .await?
        .ok_or_else(|| not_found("no schema for current view"))?;
    Ok(Json(build_response(row, dataset_id, Some(branch), false)?))
}
