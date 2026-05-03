//! Schema validation + history endpoints (Bloque E2).
//!
//! - `POST /streams/{id}/schema:validate` → validate a candidate Avro
//!   schema (and optional payload sample) against the *current* stream
//!   schema using `event_bus_control::schema_registry`.
//! - `GET  /streams/{id}/schema/history`  → list every accepted schema
//!   version (descending) so operators can audit evolution.
//!
//! Schema persistence happens inside `streams::create_stream` /
//! `streams::update_stream`; this module only exposes the read /
//! validation surface.

use axum::{
    Json,
    extract::{Path, State},
};
use event_bus_control::schema_registry::{self, CompatibilityMode, SchemaType};
use sqlx::types::Json as SqlJson;
use std::str::FromStr;
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, not_found, streams},
    models::{
        ListResponse,
        schema_history::{
            CompatibilityOutcome, StreamSchemaHistoryRow, StreamSchemaVersion,
            ValidateSchemaRequest, ValidateSchemaResponse,
        },
    },
};

pub async fn validate_schema(
    State(state): State<AppState>,
    Path(stream_id): Path<Uuid>,
    Json(payload): Json<ValidateSchemaRequest>,
) -> ServiceResult<ValidateSchemaResponse> {
    let mut errors = Vec::new();
    let mut warnings = Vec::new();

    let candidate_text = match serde_json::to_string(&payload.schema_avro) {
        Ok(text) => text,
        Err(err) => return Err(bad_request(format!("invalid JSON schema: {err}"))),
    };

    // 1. Parse / fingerprint.
    let fingerprint = match streams::compute_avro_fingerprint(&payload.schema_avro) {
        Ok(fp) => Some(fp),
        Err(err) => {
            errors.push(format!("schema parse failed: {err}"));
            None
        }
    };

    // 2. Optional payload validation.
    if let Some(sample) = payload.sample.as_ref() {
        if let Err(err) =
            schema_registry::validate_payload(SchemaType::Avro, &candidate_text, sample)
        {
            errors.push(format!("sample failed validation: {err}"));
        }
    }

    // 3. Compatibility against current persisted schema (if any).
    let mut compatibility_outcome: Option<CompatibilityOutcome> = None;
    let current: Option<(Option<SqlJson<serde_json::Value>>, String)> = sqlx::query_as(
        "SELECT schema_avro, schema_compatibility_mode
           FROM streaming_streams WHERE id = $1",
    )
    .bind(stream_id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let Some((current_schema, current_mode)) = current else {
        return Err(not_found("stream not found"));
    };

    let mode_text = payload
        .compatibility
        .clone()
        .unwrap_or(current_mode.clone());
    let mode = CompatibilityMode::from_str(&mode_text)
        .map_err(|e| bad_request(format!("invalid compatibility mode: {e}")))?;

    if let Some(prev) = current_schema {
        let previous_text = serde_json::to_string(&prev.0).unwrap_or_default();
        match schema_registry::check_compatibility(
            SchemaType::Avro,
            &previous_text,
            &candidate_text,
            mode,
        ) {
            Ok(()) => {
                compatibility_outcome = Some(CompatibilityOutcome {
                    mode: mode_text.clone(),
                    compatible: true,
                    reason: None,
                });
            }
            Err(err) => {
                compatibility_outcome = Some(CompatibilityOutcome {
                    mode: mode_text.clone(),
                    compatible: false,
                    reason: Some(err.to_string()),
                });
                errors.push(format!("compatibility violation: {err}"));
            }
        }
    } else {
        warnings.push("no current Avro schema persisted; compatibility check skipped".to_string());
    }

    let valid = errors.is_empty();
    Ok(Json(ValidateSchemaResponse {
        valid,
        fingerprint,
        errors,
        warnings,
        compatibility: compatibility_outcome,
    }))
}

pub async fn list_schema_history(
    State(state): State<AppState>,
    Path(stream_id): Path<Uuid>,
) -> ServiceResult<ListResponse<StreamSchemaVersion>> {
    let exists: bool =
        sqlx::query_scalar("SELECT EXISTS(SELECT 1 FROM streaming_streams WHERE id = $1)")
            .bind(stream_id)
            .fetch_one(&state.db)
            .await
            .map_err(|cause| db_error(&cause))?;
    if !exists {
        return Err(not_found("stream not found"));
    }
    let rows = sqlx::query_as::<_, StreamSchemaHistoryRow>(
        "SELECT id, stream_id, version, schema_avro, fingerprint, compatibility,
                created_by, created_at
           FROM streaming_stream_schema_history
          WHERE stream_id = $1
          ORDER BY version DESC",
    )
    .bind(stream_id)
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse {
        data: rows.into_iter().map(Into::into).collect(),
    }))
}
