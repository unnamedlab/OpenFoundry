//! HTTP surface for media-set sync defs (Foundry "Set up a media set
//! sync" → S3 / ABFS / OneLake sources).
//!
//! Two flavours are supported (see [`crate::domain::media_set_sync`]):
//!
//! * `MEDIA_SET_SYNC` — bytes are copied into Foundry by POSTing each
//!   accepted file to `media-sets-service` `POST /media-sets/{rid}/items/upload-url`.
//! * `VIRTUAL_MEDIA_SET_SYNC` — only metadata is registered, via
//!   `POST /media-sets/{rid}/virtual-items`. Bytes stay in the source.
//!
//! The executor here is intentionally minimal: it takes a pre-enumerated
//! list of source files (real S3 / ABFS enumeration lives in the
//! per-connector modules) and dispatches the post-filter accept-set to
//! `media-sets-service`. The user-facing flow is therefore:
//!
//! 1. `POST /sources/{rid}/media-set-syncs` persists the def.
//! 2. The connector runtime enumerates files when the cron fires (or
//!    when an operator clicks "Run").
//! 3. [`execute_media_set_sync`] applies filters + dispatches.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use uuid::Uuid;

use crate::AppState;
use crate::domain::media_set_sync::{
    MediaSetSyncConfig, MediaSetSyncFilters, MediaSetSyncKind, SourceFile, SyncDecision, SyncStats,
    classify_batch,
};

#[derive(Debug, Deserialize)]
pub struct CreateMediaSetSyncRequest {
    pub kind: MediaSetSyncKind,
    pub target_media_set_rid: String,
    #[serde(default)]
    pub subfolder: String,
    #[serde(default)]
    pub filters: MediaSetSyncFilters,
    #[serde(default)]
    pub schedule_cron: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct MediaSetSyncResponse {
    pub id: Uuid,
    pub source_id: Uuid,
    pub kind: MediaSetSyncKind,
    pub target_media_set_rid: String,
    pub subfolder: String,
    pub filters: MediaSetSyncFilters,
    pub schedule_cron: Option<String>,
    pub created_at: DateTime<Utc>,
}

pub async fn create_media_set_sync(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
    Json(body): Json<CreateMediaSetSyncRequest>,
) -> impl IntoResponse {
    let cfg = MediaSetSyncConfig {
        kind: body.kind,
        target_media_set_rid: body.target_media_set_rid.clone(),
        subfolder: body.subfolder.clone(),
        filters: body.filters.clone(),
        schedule_cron: body.schedule_cron.clone(),
    };
    let validation_errors = cfg.validate();
    if !validation_errors.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "errors": validation_errors })),
        )
            .into_response();
    }

    let id = Uuid::now_v7();
    let filters_json = serde_json::to_value(&body.filters).unwrap_or_default();
    let row = sqlx::query_as::<_, (Uuid, Uuid, String, String, String, serde_json::Value, Option<String>, DateTime<Utc>)>(
        r#"INSERT INTO media_set_syncs
              (id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron)
           VALUES ($1, $2, $3, $4, $5, $6, $7)
        RETURNING id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron, created_at"#,
    )
    .bind(id)
    .bind(source_id)
    .bind(body.kind.as_str())
    .bind(&body.target_media_set_rid)
    .bind(&body.subfolder)
    .bind(&filters_json)
    .bind(body.schedule_cron.as_deref())
    .fetch_one(&state.db)
    .await;

    match row {
        Ok((id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron, created_at)) => {
            let kind = parse_kind(&sync_type);
            let filters: MediaSetSyncFilters = serde_json::from_value(filters).unwrap_or_default();
            (
                StatusCode::CREATED,
                Json(MediaSetSyncResponse {
                    id,
                    source_id,
                    kind,
                    target_media_set_rid,
                    subfolder,
                    filters,
                    schedule_cron,
                    created_at,
                }),
            )
                .into_response()
        }
        Err(err) => {
            tracing::error!(?err, "failed to insert media_set_sync row");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": err.to_string() })),
            )
                .into_response()
        }
    }
}

pub async fn list_media_set_syncs(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, (Uuid, Uuid, String, String, String, serde_json::Value, Option<String>, DateTime<Utc>)>(
        r#"SELECT id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron, created_at
             FROM media_set_syncs
            WHERE source_id = $1
         ORDER BY created_at DESC"#,
    )
    .bind(source_id)
    .fetch_all(&state.db)
    .await;
    match rows {
        Ok(rows) => {
            let body: Vec<MediaSetSyncResponse> = rows
                .into_iter()
                .map(|(id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron, created_at)| {
                    let filters: MediaSetSyncFilters = serde_json::from_value(filters).unwrap_or_default();
                    MediaSetSyncResponse {
                        id,
                        source_id,
                        kind: parse_kind(&sync_type),
                        target_media_set_rid,
                        subfolder,
                        filters,
                        schedule_cron,
                        created_at,
                    }
                })
                .collect();
            (StatusCode::OK, Json(body)).into_response()
        }
        Err(err) => {
            tracing::error!(?err, "failed to list media_set_syncs");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": err.to_string() })),
            )
                .into_response()
        }
    }
}

fn parse_kind(s: &str) -> MediaSetSyncKind {
    match s {
        "VIRTUAL_MEDIA_SET_SYNC" => MediaSetSyncKind::VirtualMediaSetSync,
        _ => MediaSetSyncKind::MediaSetSync,
    }
}

// ---------------------------------------------------------------------------
// Executor
// ---------------------------------------------------------------------------

/// Outcome of a single executor pass: filter stats + per-file dispatch
/// outcomes in source order. The dispatch posts go to
/// `media-sets-service` (or, for virtual syncs, to its `virtual-items`
/// endpoint).
#[derive(Debug, Serialize)]
pub struct ExecutionReport {
    pub stats: SyncStats,
    pub dispatched: u32,
    pub dispatch_errors: u32,
    pub schema_mismatches: Vec<String>,
}

/// Run a single executor pass against an already-enumerated batch of
/// source files. Real S3 / ABFS enumeration is a separate concern and
/// lives in the per-connector modules; the executor only handles
/// filtering + dispatching.
///
/// `media_sets_service_url` is the base URL of `media-sets-service`
/// (e.g. `http://media-sets-service:50156`). When empty, the executor
/// classifies but does not dispatch — useful for previews and tests.
pub async fn execute_media_set_sync(
    cfg: &MediaSetSyncConfig,
    files: &[SourceFile],
    already_synced: &HashSet<String>,
    allowed_mime_types: &[String],
    http: &reqwest::Client,
    media_sets_service_url: &str,
    bearer_token: Option<&str>,
) -> Result<ExecutionReport, String> {
    let (decisions, stats) = classify_batch(cfg, files, already_synced, allowed_mime_types)?;
    let mut dispatched = 0u32;
    let mut dispatch_errors = 0u32;
    let mut schema_mismatches = Vec::new();

    for (file, decision) in decisions {
        match decision {
            SyncDecision::Skip => continue,
            SyncDecision::SchemaMismatch => {
                schema_mismatches.push(file.path.clone());
                continue;
            }
            SyncDecision::Accept => {}
        }
        if media_sets_service_url.is_empty() {
            dispatched += 1;
            continue;
        }
        let result = dispatch_file(
            cfg,
            &file,
            http,
            media_sets_service_url,
            bearer_token,
        )
        .await;
        match result {
            Ok(()) => dispatched += 1,
            Err(err) => {
                tracing::warn!(error = %err, path = %file.path, "media-set sync dispatch failed");
                dispatch_errors += 1;
            }
        }
    }

    Ok(ExecutionReport {
        stats,
        dispatched,
        dispatch_errors,
        schema_mismatches,
    })
}

async fn dispatch_file(
    cfg: &MediaSetSyncConfig,
    file: &SourceFile,
    http: &reqwest::Client,
    base: &str,
    bearer_token: Option<&str>,
) -> Result<(), String> {
    let base = base.trim_end_matches('/');
    let url = match cfg.kind {
        MediaSetSyncKind::MediaSetSync => format!(
            "{base}/media-sets/{}/items/upload-url",
            cfg.target_media_set_rid
        ),
        MediaSetSyncKind::VirtualMediaSetSync => format!(
            "{base}/media-sets/{}/virtual-items",
            cfg.target_media_set_rid
        ),
    };
    let body = match cfg.kind {
        MediaSetSyncKind::MediaSetSync => serde_json::json!({
            "path": file.path,
            "mime_type": file.mime_type,
            "size_bytes": file.size_bytes,
        }),
        MediaSetSyncKind::VirtualMediaSetSync => serde_json::json!({
            "physical_path": format!("{}/{}", cfg.subfolder.trim_end_matches('/'), file.path),
            "item_path": file.path,
            "mime_type": file.mime_type,
            "size_bytes": file.size_bytes,
        }),
    };
    let mut req = http.post(&url).json(&body);
    if let Some(token) = bearer_token {
        req = req.bearer_auth(token);
    }
    let resp = req.send().await.map_err(|e| e.to_string())?;
    if !resp.status().is_success() {
        return Err(format!("media-sets-service returned HTTP {}", resp.status().as_u16()));
    }
    Ok(())
}
