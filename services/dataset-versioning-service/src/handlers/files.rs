//! P3 — Foundry "Backing filesystem" endpoints.
//!
//! Routes:
//!   GET  /v1/datasets/{rid}/files                            — view-effective list
//!   GET  /v1/datasets/{rid}/files/{file_id}/download         — 302 → presigned URL
//!   POST /v1/datasets/{rid}/transactions/{txn}/files         — issue presigned PUT
//!   GET  /v1/_internal/local-fs/*key                         — local presign proxy
//!
//! The first three follow the Foundry doc § "Backing filesystem"
//! contract: a logical_path → physical_uri mapping in
//! [`dataset_files`] (P3 migration), with view-time visibility
//! resolved through the same SNAPSHOT/APPEND/UPDATE/DELETE algorithm
//! the rest of the service already uses.

use std::collections::BTreeMap;
use std::time::Duration;

use auth_middleware::layer::AuthUser;
use axum::Json;
use axum::extract::{Path, Query, State};
use axum::http::{HeaderValue, StatusCode, header};
use axum::response::Response;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use storage_abstraction::backing_fs::PhysicalLocation;
use uuid::Uuid;

use crate::AppState;

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct ListFilesQuery {
    #[serde(default)]
    pub branch: Option<String>,
    #[serde(default)]
    pub view_id: Option<Uuid>,
    #[serde(default)]
    pub prefix: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct DatasetFileOut {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub transaction_id: Uuid,
    pub logical_path: String,
    pub physical_uri: String,
    pub size_bytes: i64,
    pub sha256: Option<String>,
    pub created_at: DateTime<Utc>,
    pub modified_at: DateTime<Utc>,
    /// `active` when the file is visible in the resolved view,
    /// `deleted` when soft-deleted.
    pub status: &'static str,
}

#[derive(Debug, Serialize)]
pub struct ListFilesOut {
    pub view_id: Option<Uuid>,
    pub branch: String,
    pub total: usize,
    pub files: Vec<DatasetFileOut>,
}

#[derive(Debug, Deserialize)]
pub struct UploadUrlBody {
    /// Dataset-relative logical path the caller intends to PUT to.
    pub logical_path: String,
    /// Optional content-type recorded for auditing; not enforced at
    /// presigning time.
    #[serde(default)]
    pub content_type: Option<String>,
    /// Optional SHA-256 hex of the bytes being uploaded. Round-trips
    /// to the eventual `dataset_files.sha256` row.
    #[serde(default)]
    pub sha256: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct UploadUrlOut {
    pub url: String,
    pub physical_uri: String,
    pub expires_at: DateTime<Utc>,
    pub method: &'static str,
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "dataset-versioning-service: files handler error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
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

fn parse_physical_uri(uri: &str) -> PhysicalLocation {
    // Inverse of `PhysicalLocation::uri()`. We keep this loose — bad
    // URIs become `local://` placeholders so the download path can
    // surface a 4xx rather than panicking on legacy rows.
    let (fs_id, rest) = if let Some(rest) = uri.strip_prefix("s3://") {
        match rest.split_once('/') {
            Some((bucket, tail)) => (format!("s3:{bucket}"), tail.to_string()),
            None => (format!("s3:{rest}"), String::new()),
        }
    } else if let Some(rest) = uri.strip_prefix("hdfs://") {
        match rest.split_once('/') {
            Some((host, tail)) => (format!("hdfs:{host}"), tail.to_string()),
            None => (format!("hdfs:{rest}"), String::new()),
        }
    } else if let Some(rest) = uri.strip_prefix("local://") {
        ("local".to_string(), rest.trim_start_matches('/').to_string())
    } else {
        ("local".to_string(), uri.trim_start_matches('/').to_string())
    };
    // We don't have base_dir vs relative_path on disk; treat the whole
    // tail as `relative_path` and leave base_dir empty. The trait impls
    // accept this (object_key joins them).
    PhysicalLocation {
        fs_id,
        base_dir: String::new(),
        relative_path: rest,
        version_token: None,
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// View-effective listing.
//
// Per the doc's algorithm we walk committed transactions in commit
// order and apply each one's staged ops. The resulting BTreeMap keyed
// on logical_path holds the active set; soft-deleted files appear as
// status='deleted' so the UI can surface the badge.
// ─────────────────────────────────────────────────────────────────────────────

#[derive(sqlx::FromRow)]
struct FileRow {
    id: Uuid,
    transaction_id: Uuid,
    logical_path: String,
    physical_uri: String,
    size_bytes: i64,
    sha256: Option<String>,
    created_at: DateTime<Utc>,
    op: String,
    tx_committed_at: Option<DateTime<Utc>>,
    tx_started_at: DateTime<Utc>,
    tx_type: String,
}

pub async fn list_files(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Query(params): Query<ListFilesQuery>,
) -> Result<Json<ListFilesOut>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let branch = params.branch.unwrap_or_else(|| "master".into());

    // Resolve the head txn for the branch (or, if view_id is given,
    // the view's head).
    let head_txn: Option<Uuid> = match params.view_id {
        Some(vid) => sqlx::query_scalar::<_, Uuid>(
            r#"SELECT head_transaction_id FROM dataset_views WHERE id = $1 AND dataset_id = $2"#,
        )
        .bind(vid)
        .bind(dataset_id)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?,
        None => sqlx::query_scalar::<_, Option<Uuid>>(
            r#"SELECT head_transaction_id FROM dataset_branches
                WHERE dataset_id = $1 AND name = $2 AND deleted_at IS NULL"#,
        )
        .bind(dataset_id)
        .bind(&branch)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?
        .flatten(),
    };

    let Some(head_txn) = head_txn else {
        return Ok(Json(ListFilesOut {
            view_id: params.view_id,
            branch,
            total: 0,
            files: Vec::new(),
        }));
    };

    // Load every staged file row for committed transactions on this
    // branch up to and including `head_txn`. The `op` column drives the
    // SNAPSHOT/APPEND/UPDATE/DELETE algorithm below; we lean on
    // dataset_transaction_files (the staging table) because
    // dataset_files isn't filtered by txn ordering.
    let cutoff = sqlx::query_scalar::<_, Option<DateTime<Utc>>>(
        r#"SELECT COALESCE(committed_at, started_at)
             FROM dataset_transactions WHERE id = $1"#,
    )
    .bind(head_txn)
    .fetch_one(&state.db)
    .await
    .map_err(internal)?;

    let rows: Vec<FileRow> = sqlx::query_as::<_, FileRow>(
        r#"SELECT
              df.id                                              AS id,
              t.id                                               AS transaction_id,
              df.logical_path                                    AS logical_path,
              df.physical_uri                                    AS physical_uri,
              df.size_bytes                                      AS size_bytes,
              df.sha256                                          AS sha256,
              df.created_at                                      AS created_at,
              tf.op                                              AS op,
              t.committed_at                                     AS tx_committed_at,
              t.started_at                                       AS tx_started_at,
              t.tx_type                                          AS tx_type
            FROM dataset_files df
            JOIN dataset_transactions t ON t.id = df.transaction_id
            JOIN dataset_transaction_files tf
                  ON tf.transaction_id = df.transaction_id
                 AND tf.logical_path = df.logical_path
           WHERE df.dataset_id = $1
             AND t.status = 'COMMITTED'
             AND ($2::timestamptz IS NULL
                   OR COALESCE(t.committed_at, t.started_at) <= $2)
           ORDER BY COALESCE(t.committed_at, t.started_at) ASC, df.created_at ASC"#,
    )
    .bind(dataset_id)
    .bind(cutoff)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;

    // Apply the doc's algorithm. We track the *latest* dataset_files
    // row for every logical_path so the response gives a single
    // canonical id per logical path (the soft-deleted set carries its
    // own bucket so the UI can render the "deleted in current view"
    // badge for files removed by an UPDATE/DELETE).
    let mut active: BTreeMap<String, FileRow> = BTreeMap::new();
    let mut deleted: BTreeMap<String, FileRow> = BTreeMap::new();
    let mut started = false;
    for row in rows {
        if !started && row.tx_type == "SNAPSHOT" {
            active.clear();
            deleted.clear();
            started = true;
        }
        match row.tx_type.as_str() {
            "SNAPSHOT" => {
                active.insert(row.logical_path.clone(), row);
            }
            "APPEND" => {
                active.entry(row.logical_path.clone()).or_insert(row);
            }
            "UPDATE" => {
                if row.op == "REMOVE" {
                    if let Some(prev) = active.remove(&row.logical_path) {
                        deleted.insert(prev.logical_path.clone(), prev);
                    }
                } else {
                    active.insert(row.logical_path.clone(), row);
                }
            }
            "DELETE" => {
                if let Some(prev) = active.remove(&row.logical_path) {
                    deleted.insert(prev.logical_path.clone(), prev);
                }
            }
            _ => {}
        }
    }

    // Optional prefix filter — applied after view resolution so the
    // SNAPSHOT/etc. algorithm sees the full picture.
    let prefix = params.prefix.as_deref().unwrap_or("");
    let mut files: Vec<DatasetFileOut> = active
        .into_values()
        .filter(|f| prefix.is_empty() || f.logical_path.starts_with(prefix))
        .map(|f| to_out(f, "active"))
        .collect();
    files.extend(
        deleted
            .into_values()
            .filter(|f| prefix.is_empty() || f.logical_path.starts_with(prefix))
            .map(|f| to_out(f, "deleted")),
    );
    files.sort_by(|a, b| a.logical_path.cmp(&b.logical_path));

    Ok(Json(ListFilesOut {
        view_id: params.view_id,
        branch,
        total: files.len(),
        files,
    }))
}

fn to_out(row: FileRow, status: &'static str) -> DatasetFileOut {
    DatasetFileOut {
        id: row.id,
        dataset_id: Uuid::nil(), // populated by caller; saves a join
        transaction_id: row.transaction_id,
        logical_path: row.logical_path,
        physical_uri: row.physical_uri,
        size_bytes: row.size_bytes,
        sha256: row.sha256,
        created_at: row.created_at,
        modified_at: row.tx_committed_at.unwrap_or(row.tx_started_at),
        status,
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Download — 302 to a presigned URL.
// ─────────────────────────────────────────────────────────────────────────────

pub async fn download_file(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, file_id_str)): Path<(String, String)>,
) -> Result<Response, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let file_id =
        Uuid::parse_str(&file_id_str).map_err(|_| bad_request("file_id is not a valid UUID"))?;

    // Lookup the row + verify it belongs to this dataset (also rejects
    // soft-deleted files: they're listed by the GET endpoint with
    // status='deleted' but cannot be downloaded).
    #[derive(sqlx::FromRow)]
    struct Row {
        physical_uri: String,
        deleted_at: Option<DateTime<Utc>>,
        logical_path: String,
        size_bytes: i64,
    }
    let row = sqlx::query_as::<_, Row>(
        r#"SELECT physical_uri, deleted_at, logical_path, size_bytes
             FROM dataset_files
            WHERE id = $1 AND dataset_id = $2"#,
    )
    .bind(file_id)
    .bind(dataset_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?;
    let row = row.ok_or_else(|| not_found("file not found"))?;
    if row.deleted_at.is_some() {
        return Err((
            StatusCode::GONE,
            Json(json!({
                "error": "file is soft-deleted in the current view",
                "logical_path": row.logical_path,
            })),
        ));
    }

    let physical = parse_physical_uri(&row.physical_uri);
    let ttl = Duration::from_secs(state.presign_ttl_seconds);
    let signed = state
        .backing_fs
        .presigned_url(&physical, ttl)
        .await
        .map_err(|e| internal(format!("presign failed: {e}")))?;

    crate::security::emit_audit(
        &user.0.sub,
        "files.download",
        &rid,
        json!({
            "file_id": file_id,
            "logical_path": row.logical_path,
            "size_bytes": row.size_bytes,
            "physical_uri": row.physical_uri,
            "presign_ttl_seconds": state.presign_ttl_seconds,
            "expires_at": signed.expires_at,
        }),
    );

    let mut response = Response::builder()
        .status(StatusCode::FOUND)
        .body(axum::body::Body::empty())
        .map_err(|e| internal(e.to_string()))?;
    response.headers_mut().insert(
        header::LOCATION,
        HeaderValue::from_str(signed.url.as_str())
            .map_err(|e| internal(e.to_string()))?,
    );
    response.headers_mut().insert(
        header::CACHE_CONTROL,
        HeaderValue::from_static("private, max-age=0, must-revalidate"),
    );
    Ok(response)
}

// ─────────────────────────────────────────────────────────────────────────────
// Upload-URL — caller PUTs bytes directly to the backing FS.
// ─────────────────────────────────────────────────────────────────────────────

pub async fn upload_url(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, txn_str)): Path<(String, String)>,
    Json(body): Json<UploadUrlBody>,
) -> Result<Json<UploadUrlOut>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "files.upload_url")?;

    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let txn_id =
        Uuid::parse_str(&txn_str).map_err(|_| bad_request("transaction id is not a valid UUID"))?;
    let logical = body.logical_path.trim();
    if logical.is_empty() {
        return Err(bad_request("logical_path must not be empty"));
    }

    // Sanity: the txn must exist and belong to this dataset and be
    // OPEN. Foundry semantics: callers can't stage files on a
    // committed/aborted transaction.
    #[derive(sqlx::FromRow)]
    struct Row {
        status: String,
    }
    let row = sqlx::query_as::<_, Row>(
        r#"SELECT status FROM dataset_transactions
            WHERE id = $1 AND dataset_id = $2"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .ok_or_else(|| not_found("transaction not found"))?;
    if row.status != "OPEN" {
        return Err((
            StatusCode::CONFLICT,
            Json(json!({
                "error": "transaction is not OPEN",
                "current_state": row.status,
            })),
        ));
    }

    // Reserve a deterministic logical path under the transaction so
    // multiple uploads in the same txn don't collide.
    let logical_relative = format!("transactions/{txn_id}/{}", logical.trim_start_matches('/'));
    let physical = PhysicalLocation {
        fs_id: state.backing_fs.fs_id().to_string(),
        base_dir: state.backing_fs.base_directory().to_string(),
        relative_path: logical_relative.clone(),
        version_token: None,
    };
    let ttl = Duration::from_secs(state.presign_ttl_seconds);
    let signed = state
        .backing_fs
        .presigned_url(&physical, ttl)
        .await
        .map_err(|e| internal(format!("presign failed: {e}")))?;

    crate::security::emit_audit(
        &user.0.sub,
        "files.upload_url",
        &rid,
        json!({
            "transaction_id": txn_id,
            "logical_path": logical_relative,
            "content_type": body.content_type,
            "sha256": body.sha256,
            "physical_uri": physical.uri(),
            "presign_ttl_seconds": state.presign_ttl_seconds,
            "expires_at": signed.expires_at,
        }),
    );

    Ok(Json(UploadUrlOut {
        url: signed.url.to_string(),
        physical_uri: physical.uri(),
        expires_at: signed.expires_at,
        method: "PUT",
    }))
}

// ─────────────────────────────────────────────────────────────────────────────
// Local presigned download proxy. Verifies the HMAC + expiry against
// the LocalBackingFs config, then streams the bytes out. S3 / HDFS
// presigned URLs are served by the backing service directly and never
// hit this route.
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct LocalProxyQuery {
    pub expires: i64,
    pub sig: String,
}

// ─────────────────────────────────────────────────────────────────────────────
// Storage details — surfaces the running BackingFsConfig (driver,
// base_dir, bucket, region, GB consumed). Used by the manage-only
// `Storage details` UI tab.
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Serialize)]
pub struct StorageDetailsOut {
    pub fs_id: String,
    pub driver: &'static str,
    pub base_directory: String,
    pub presign_ttl_seconds: u64,
    /// Sum of `size_bytes` for active rows in `dataset_files`.
    pub total_active_bytes: i64,
    pub total_active_files: i64,
    /// Sum of `size_bytes` for soft-deleted rows. Surfaced separately
    /// so manage users can see how much storage is queued for retention.
    pub total_deleted_bytes: i64,
    pub total_deleted_files: i64,
}

pub async fn storage_details(
    State(state): State<AppState>,
    user: AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<StorageDetailsOut>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "storage.details")?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;

    let driver: &'static str = if state.backing_fs.fs_id().starts_with("s3:") {
        "s3"
    } else if state.backing_fs.fs_id().starts_with("hdfs:") {
        "hdfs"
    } else {
        "local"
    };

    #[derive(sqlx::FromRow)]
    struct Totals {
        total_active_bytes: Option<i64>,
        total_active_files: i64,
        total_deleted_bytes: Option<i64>,
        total_deleted_files: i64,
    }
    let totals = sqlx::query_as::<_, Totals>(
        r#"SELECT
              SUM(CASE WHEN deleted_at IS NULL     THEN size_bytes ELSE 0 END) AS total_active_bytes,
              COUNT(*) FILTER (WHERE deleted_at IS NULL)                       AS total_active_files,
              SUM(CASE WHEN deleted_at IS NOT NULL THEN size_bytes ELSE 0 END) AS total_deleted_bytes,
              COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)                   AS total_deleted_files
            FROM dataset_files
           WHERE dataset_id = $1"#,
    )
    .bind(dataset_id)
    .fetch_one(&state.db)
    .await
    .map_err(internal)?;

    Ok(Json(StorageDetailsOut {
        fs_id: state.backing_fs.fs_id().to_string(),
        driver,
        base_directory: state.backing_fs.base_directory().to_string(),
        presign_ttl_seconds: state.presign_ttl_seconds,
        total_active_bytes: totals.total_active_bytes.unwrap_or(0),
        total_active_files: totals.total_active_files,
        total_deleted_bytes: totals.total_deleted_bytes.unwrap_or(0),
        total_deleted_files: totals.total_deleted_files,
    }))
}

pub async fn local_presign_proxy(
    State(state): State<AppState>,
    Path(key): Path<String>,
    Query(q): Query<LocalProxyQuery>,
) -> Result<Response, (StatusCode, Json<Value>)> {
    if !state.backing_fs.verify_local_signature(&key, q.expires, &q.sig) {
        return Err((
            StatusCode::FORBIDDEN,
            Json(json!({ "error": "invalid or expired signature" })),
        ));
    }
    let bytes = state
        .storage
        .get(&key)
        .await
        .map_err(|e| internal(format!("storage get: {e}")))?;
    let mut response = Response::builder()
        .status(StatusCode::OK)
        .body(axum::body::Body::from(bytes))
        .map_err(|e| internal(e.to_string()))?;
    response.headers_mut().insert(
        header::CONTENT_TYPE,
        HeaderValue::from_static("application/octet-stream"),
    );
    Ok(response)
}
