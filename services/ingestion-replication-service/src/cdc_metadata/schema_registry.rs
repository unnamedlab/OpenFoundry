//! Schema Registry: storage + Confluent-compatible REST surface.
//!
//! Routes mirror the subset of the Confluent Schema Registry HTTP API that
//! the platform needs today, so existing client SDKs work unmodified:
//!
//! - `GET    /subjects`
//! - `GET    /subjects/:name/versions`
//! - `POST   /subjects/:name/versions`              register a new version
//! - `GET    /subjects/:name/versions/:version`     fetch a specific version
//!                                                  ("latest" alias supported)
//! - `POST   /compatibility/subjects/:name/versions/:version`
//!                                                  test compatibility
//!
//! Storage lives in the four tables created by
//! `migrations/20260501120000_schema_registry.sql`. The validators and
//! compatibility checks are delegated to
//! `event_bus_control::schema_registry`.

use std::str::FromStr;

use axum::{
    Json, Router,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
};
use chrono::{DateTime, Utc};
use event_bus_control::schema_registry::{
    self as validator, CompatibilityMode, SchemaError, SchemaType,
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use sqlx::PgPool;
use uuid::Uuid;

use crate::cdc_metadata::AppState;

pub fn routes() -> Router<AppState> {
    Router::new()
        .route("/subjects", get(list_subjects))
        .route(
            "/subjects/:name/versions",
            get(list_versions).post(register_version),
        )
        .route("/subjects/:name/versions/:version", get(get_version))
        .route(
            "/compatibility/subjects/:name/versions/:version",
            post(check_compatibility),
        )
}

// ---------- DTOs (Confluent-compatible shapes) ----------

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
struct SubjectRow {
    id: Uuid,
    name: String,
    compatibility_mode: String,
    created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
struct VersionRow {
    id: Uuid,
    subject_id: Uuid,
    version: i32,
    schema_type: String,
    schema_text: String,
    fingerprint: String,
    created_at: DateTime<Utc>,
    deprecated_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Deserialize)]
pub struct RegisterVersionRequest {
    pub schema: String,
    #[serde(
        rename = "schemaType",
        alias = "schema_type",
        default = "default_schema_type"
    )]
    pub schema_type: String,
    #[serde(default)]
    pub references: Vec<SchemaReference>,
}

fn default_schema_type() -> String {
    "AVRO".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SchemaReference {
    pub name: String,
    pub subject: String,
    pub version: i32,
}

#[derive(Debug, Serialize)]
struct RegisterVersionResponse {
    id: i32,
}

#[derive(Debug, Serialize)]
struct VersionResponse {
    subject: String,
    id: i32,
    version: i32,
    schema: String,
    #[serde(rename = "schemaType")]
    schema_type: String,
}

#[derive(Debug, Deserialize)]
pub struct CompatibilityCheckRequest {
    pub schema: String,
    #[serde(
        rename = "schemaType",
        alias = "schema_type",
        default = "default_schema_type"
    )]
    pub schema_type: String,
}

#[derive(Debug, Serialize)]
struct CompatibilityCheckResponse {
    is_compatible: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    messages: Option<Vec<String>>,
}

// ---------- helpers ----------

fn db_error(label: &'static str, error: sqlx::Error) -> axum::response::Response {
    tracing::error!("schema_registry {label} failed: {error}");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({
            "error_code": 50001,
            "message": format!("{label} failed"),
        })),
    )
        .into_response()
}

fn schema_error(error: SchemaError) -> axum::response::Response {
    let status = match &error {
        SchemaError::Parse(_) | SchemaError::UnsupportedSchemaType(_) => {
            StatusCode::UNPROCESSABLE_ENTITY
        }
        SchemaError::UnsupportedCompatibility(_) => StatusCode::BAD_REQUEST,
        SchemaError::Validation(_) | SchemaError::Compatibility(_) => StatusCode::CONFLICT,
    };
    (
        status,
        Json(json!({
            "error_code": match status {
                StatusCode::UNPROCESSABLE_ENTITY => 42201,
                StatusCode::CONFLICT => 40901,
                _ => 40001,
            },
            "message": error.to_string(),
        })),
    )
        .into_response()
}

fn parse_schema_type(value: &str) -> Result<SchemaType, axum::response::Response> {
    SchemaType::from_str(value).map_err(schema_error)
}

async fn fetch_or_create_subject(db: &PgPool, name: &str) -> Result<SubjectRow, sqlx::Error> {
    if let Some(existing) = sqlx::query_as::<_, SubjectRow>(
        "SELECT id, name, compatibility_mode, created_at FROM schema_subjects WHERE name = $1",
    )
    .bind(name)
    .fetch_optional(db)
    .await?
    {
        return Ok(existing);
    }
    sqlx::query_as::<_, SubjectRow>(
        "INSERT INTO schema_subjects (id, name) VALUES ($1, $2)
         RETURNING id, name, compatibility_mode, created_at",
    )
    .bind(Uuid::now_v7())
    .bind(name)
    .fetch_one(db)
    .await
}

async fn latest_version(db: &PgPool, subject_id: Uuid) -> Result<Option<VersionRow>, sqlx::Error> {
    sqlx::query_as::<_, VersionRow>(
        "SELECT id, subject_id, version, schema_type, schema_text, fingerprint, created_at, deprecated_at
         FROM schema_versions
         WHERE subject_id = $1
         ORDER BY version DESC
         LIMIT 1",
    )
    .bind(subject_id)
    .fetch_optional(db)
    .await
}

async fn version_by_number(
    db: &PgPool,
    subject_id: Uuid,
    version: i32,
) -> Result<Option<VersionRow>, sqlx::Error> {
    sqlx::query_as::<_, VersionRow>(
        "SELECT id, subject_id, version, schema_type, schema_text, fingerprint, created_at, deprecated_at
         FROM schema_versions
         WHERE subject_id = $1 AND version = $2",
    )
    .bind(subject_id)
    .bind(version)
    .fetch_optional(db)
    .await
}

async fn version_by_fingerprint(
    db: &PgPool,
    subject_id: Uuid,
    fingerprint: &str,
) -> Result<Option<VersionRow>, sqlx::Error> {
    sqlx::query_as::<_, VersionRow>(
        "SELECT id, subject_id, version, schema_type, schema_text, fingerprint, created_at, deprecated_at
         FROM schema_versions
         WHERE subject_id = $1 AND fingerprint = $2",
    )
    .bind(subject_id)
    .bind(fingerprint)
    .fetch_optional(db)
    .await
}

fn version_response(subject: &str, row: &VersionRow) -> VersionResponse {
    VersionResponse {
        subject: subject.to_string(),
        // Confluent's `id` is a global, monotonically-increasing schema id;
        // we surface the per-subject version twice for client compat. The
        // distinction does not matter for in-house consumers and avoids a
        // separate sequence table.
        id: row.version,
        version: row.version,
        schema: row.schema_text.clone(),
        schema_type: row.schema_type.to_uppercase(),
    }
}

// ---------- handlers ----------

async fn list_subjects(State(state): State<AppState>) -> impl IntoResponse {
    let result = sqlx::query_scalar::<_, String>("SELECT name FROM schema_subjects ORDER BY name")
        .fetch_all(&state.db)
        .await;
    match result {
        Ok(names) => Json(names).into_response(),
        Err(error) => db_error("list_subjects", error),
    }
}

async fn list_versions(
    State(state): State<AppState>,
    Path(name): Path<String>,
) -> impl IntoResponse {
    let subject = match sqlx::query_as::<_, SubjectRow>(
        "SELECT id, name, compatibility_mode, created_at FROM schema_subjects WHERE name = $1",
    )
    .bind(&name)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => row,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => return db_error("list_versions/subject", error),
    };
    match sqlx::query_scalar::<_, i32>(
        "SELECT version FROM schema_versions WHERE subject_id = $1 ORDER BY version",
    )
    .bind(subject.id)
    .fetch_all(&state.db)
    .await
    {
        Ok(versions) => Json(versions).into_response(),
        Err(error) => db_error("list_versions", error),
    }
}

async fn get_version(
    State(state): State<AppState>,
    Path((name, version)): Path<(String, String)>,
) -> impl IntoResponse {
    let subject = match sqlx::query_as::<_, SubjectRow>(
        "SELECT id, name, compatibility_mode, created_at FROM schema_subjects WHERE name = $1",
    )
    .bind(&name)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => row,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => return db_error("get_version/subject", error),
    };
    let row = if version.eq_ignore_ascii_case("latest") {
        match latest_version(&state.db, subject.id).await {
            Ok(value) => value,
            Err(error) => return db_error("get_version/latest", error),
        }
    } else {
        let parsed: i32 = match version.parse() {
            Ok(v) => v,
            Err(_) => return StatusCode::BAD_REQUEST.into_response(),
        };
        match version_by_number(&state.db, subject.id, parsed).await {
            Ok(value) => value,
            Err(error) => return db_error("get_version", error),
        }
    };
    match row {
        Some(row) => Json(version_response(&subject.name, &row)).into_response(),
        None => StatusCode::NOT_FOUND.into_response(),
    }
}

async fn register_version(
    State(state): State<AppState>,
    Path(name): Path<String>,
    Json(body): Json<RegisterVersionRequest>,
) -> impl IntoResponse {
    let schema_type = match parse_schema_type(&body.schema_type) {
        Ok(value) => value,
        Err(response) => return response,
    };
    let fingerprint = match validator::fingerprint(schema_type, &body.schema) {
        Ok(value) => value,
        Err(error) => return schema_error(error),
    };
    // Always validate that the schema text *parses* by running an empty
    // compatibility check against itself; reuses the parser path without
    // duplicating per-type code.
    if let Err(error) = validator::check_compatibility(
        schema_type,
        &body.schema,
        &body.schema,
        CompatibilityMode::None,
    ) {
        return schema_error(error);
    }

    let subject = match fetch_or_create_subject(&state.db, &name).await {
        Ok(value) => value,
        Err(error) => return db_error("register_version/subject", error),
    };

    // Idempotency: if the same canonical schema is already registered for
    // this subject, return the existing version (Confluent semantics).
    if let Ok(Some(existing)) = version_by_fingerprint(&state.db, subject.id, &fingerprint).await {
        return (
            StatusCode::OK,
            Json(RegisterVersionResponse {
                id: existing.version,
            }),
        )
            .into_response();
    }

    // Compatibility gate: compare against the latest version (if any)
    // under the subject's configured mode.
    let mode = match CompatibilityMode::from_str(&subject.compatibility_mode) {
        Ok(mode) => mode,
        Err(error) => return schema_error(error),
    };
    let latest = match latest_version(&state.db, subject.id).await {
        Ok(value) => value,
        Err(error) => return db_error("register_version/latest", error),
    };

    let mut compat_outcome = "compatible";
    let mut compat_detail: Option<String> = None;
    if let Some(prev) = &latest {
        if let Err(error) =
            validator::check_compatibility(schema_type, &prev.schema_text, &body.schema, mode)
        {
            compat_outcome = "incompatible";
            compat_detail = Some(error.to_string());
            // Audit before returning.
            let _ = sqlx::query(
                "INSERT INTO schema_compatibility_audit (id, subject_id, subject_name,
                                                        candidate_fingerprint, previous_version,
                                                        compatibility_mode, outcome, detail)
                 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
            )
            .bind(Uuid::now_v7())
            .bind(subject.id)
            .bind(&subject.name)
            .bind(&fingerprint)
            .bind(prev.version)
            .bind(&subject.compatibility_mode)
            .bind(compat_outcome)
            .bind(compat_detail.as_deref())
            .execute(&state.db)
            .await;
            return schema_error(error);
        }
    }

    let next_version = latest.as_ref().map(|row| row.version + 1).unwrap_or(1);
    let inserted = sqlx::query_as::<_, VersionRow>(
        "INSERT INTO schema_versions (id, subject_id, version, schema_type, schema_text, fingerprint)
         VALUES ($1, $2, $3, $4, $5, $6)
         RETURNING id, subject_id, version, schema_type, schema_text, fingerprint, created_at, deprecated_at",
    )
    .bind(Uuid::now_v7())
    .bind(subject.id)
    .bind(next_version)
    .bind(schema_type.as_str())
    .bind(&body.schema)
    .bind(&fingerprint)
    .fetch_one(&state.db)
    .await;

    let row = match inserted {
        Ok(row) => row,
        Err(error) => return db_error("register_version/insert", error),
    };

    for reference in &body.references {
        let _ = sqlx::query(
            "INSERT INTO schema_references (version_id, ref_subject, ref_version)
             VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
        )
        .bind(row.id)
        .bind(&reference.subject)
        .bind(reference.version)
        .execute(&state.db)
        .await;
    }

    let _ = sqlx::query(
        "INSERT INTO schema_compatibility_audit (id, subject_id, subject_name,
                                                candidate_fingerprint, previous_version,
                                                compatibility_mode, outcome, detail)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
    )
    .bind(Uuid::now_v7())
    .bind(subject.id)
    .bind(&subject.name)
    .bind(&fingerprint)
    .bind(latest.as_ref().map(|row| row.version))
    .bind(&subject.compatibility_mode)
    .bind(compat_outcome)
    .bind(compat_detail.as_deref())
    .execute(&state.db)
    .await;

    (
        StatusCode::OK,
        Json(RegisterVersionResponse { id: row.version }),
    )
        .into_response()
}

async fn check_compatibility(
    State(state): State<AppState>,
    Path((name, version)): Path<(String, String)>,
    Json(body): Json<CompatibilityCheckRequest>,
) -> impl IntoResponse {
    let schema_type = match parse_schema_type(&body.schema_type) {
        Ok(value) => value,
        Err(response) => return response,
    };
    let subject = match sqlx::query_as::<_, SubjectRow>(
        "SELECT id, name, compatibility_mode, created_at FROM schema_subjects WHERE name = $1",
    )
    .bind(&name)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => row,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => return db_error("compatibility/subject", error),
    };
    let baseline = if version.eq_ignore_ascii_case("latest") {
        match latest_version(&state.db, subject.id).await {
            Ok(value) => value,
            Err(error) => return db_error("compatibility/latest", error),
        }
    } else {
        let parsed: i32 = match version.parse() {
            Ok(v) => v,
            Err(_) => return StatusCode::BAD_REQUEST.into_response(),
        };
        match version_by_number(&state.db, subject.id, parsed).await {
            Ok(value) => value,
            Err(error) => return db_error("compatibility/by_version", error),
        }
    };
    let baseline = match baseline {
        Some(row) => row,
        None => return StatusCode::NOT_FOUND.into_response(),
    };
    let mode = match CompatibilityMode::from_str(&subject.compatibility_mode) {
        Ok(mode) => mode,
        Err(error) => return schema_error(error),
    };
    match validator::check_compatibility(schema_type, &baseline.schema_text, &body.schema, mode) {
        Ok(()) => Json(CompatibilityCheckResponse {
            is_compatible: true,
            messages: None,
        })
        .into_response(),
        Err(error) => Json(CompatibilityCheckResponse {
            is_compatible: false,
            messages: Some(vec![error.to_string()]),
        })
        .into_response(),
    }
}
