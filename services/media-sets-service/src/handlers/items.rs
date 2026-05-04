//! Media items: presigned upload/download URLs, list, get, delete.
//!
//! Path-deduplication ("Importing media.md") happens at upload-URL
//! issuance: when a new item collides with an existing live item at the
//! same `(media_set_rid, branch, path)`, the previous one is soft-deleted
//! and the new one stores its RID in `deduplicated_from`. Both writes
//! happen in the same SQL transaction so the partial unique index never
//! sees an inconsistent intermediate state.

use std::time::Duration;

use audit_trail::events::{AuditContext, AuditEvent, emit as emit_audit};
use axum::{
    Json,
    extract::{Path, Query, State},
    http::{HeaderMap, StatusCode},
};
use serde::Deserialize;
use serde_json::Value;
use uuid::Uuid;

use crate::AppState;
use crate::domain::cedar::{
    action_item_delete, action_item_read, action_item_write, action_manage, action_view,
    check_media_item, check_media_set, mint_presign_claim,
};
use crate::domain::dedup::soft_delete_previous_at_path;
use crate::domain::error::{MediaError, MediaResult};
use crate::domain::path::{MediaItemKey, storage_uri};
use crate::domain::storage::PresignedUrl;
use crate::handlers::audit::from_request;
use crate::handlers::media_sets::{MediaErrorResponse, get_media_set_op};
use crate::metrics::{
    MEDIA_SET_DOWNLOADS_TOTAL, MEDIA_SET_STORAGE_BYTES, MEDIA_SET_UPLOADS_TOTAL,
    MEDIA_STORAGE_BYTES_LABELLED,
};
use crate::models::{MediaItem, NewMediaItem, PresignedUploadRequest, PresignedUrlBody};

pub const MEDIA_ITEM_RID_PREFIX: &str = "ri.foundry.main.media_item.";

pub fn new_media_item_rid() -> String {
    format!("{}{}", MEDIA_ITEM_RID_PREFIX, Uuid::now_v7())
}

/// Convert the legacy empty-string `transaction_rid` placeholder into
/// `None` at the bind site so the FK added in `0006_branching.sql`
/// fires only when there is a real transaction to point at.
fn bind_transaction_rid(value: &str) -> Option<&str> {
    if value.is_empty() { None } else { Some(value) }
}

/// Effective markings the Cedar engine evaluates against for `item`:
/// the parent set's markings unioned with the item's per-item override
/// (granular Foundry contract). Surfaced into audit envelopes so SIEM
/// rules see the same set the policy engine evaluated.
fn effective_item_markings(parent: &crate::models::MediaSet, item: &MediaItem) -> Vec<String> {
    let mut effective: std::collections::BTreeSet<String> = parent
        .markings
        .iter()
        .map(|m| m.to_ascii_lowercase())
        .collect();
    for marking in &item.markings {
        effective.insert(marking.to_ascii_lowercase());
    }
    effective.into_iter().collect()
}

// ---------------------------------------------------------------------------
// Operations (shared with gRPC)
// ---------------------------------------------------------------------------

/// Register a new MediaItem row, applying path dedup, and return both
/// the persisted row and a presigned PUT URL pointing at the storage
/// key derived from `(media_set_rid, branch, sha256)`.
pub async fn presigned_upload_op(
    state: &AppState,
    media_set_rid: &str,
    req: PresignedUploadRequest,
    ctx: &AuditContext,
) -> MediaResult<(MediaItem, PresignedUrl)> {
    if req.path.trim().is_empty() {
        return Err(MediaError::BadRequest("path must not be empty".into()));
    }
    let set = get_media_set_op(state, media_set_rid).await?;
    let branch = req.branch.clone().unwrap_or_else(|| "main".to_string());
    let transaction_rid = match set.transaction_policy.as_str() {
        "TRANSACTIONAL" => {
            let txn = req.transaction_rid.clone().ok_or_else(|| {
                MediaError::BadRequest("transactional media set requires a transaction_rid".into())
            })?;
            // Ensure the named transaction exists, is on the right branch
            // and is still OPEN — otherwise the upload would land in a
            // sealed batch.
            let row: Option<(String, String)> = sqlx::query_as(
                "SELECT branch, state FROM media_set_transactions WHERE rid = $1 AND media_set_rid = $2",
            )
            .bind(&txn)
            .bind(media_set_rid)
            .fetch_optional(state.db.reader())
            .await?;
            let (txn_branch, txn_state) =
                row.ok_or_else(|| MediaError::TransactionNotFound(txn.clone()))?;
            if txn_branch != branch {
                return Err(MediaError::BadRequest(format!(
                    "transaction `{txn}` is on branch `{txn_branch}`, not `{branch}`"
                )));
            }
            if txn_state != "OPEN" {
                return Err(MediaError::TransactionTerminal(txn, txn_state));
            }
            // Foundry per-transaction cap (Advanced media set
            // settings.md → "A maximum of 10,000 items can be
            // written in a single transaction"). We count live items
            // in the current transaction (deleted_at IS NULL is the
            // canonical "alive" predicate; rows soft-deleted by
            // path-dedup do NOT count toward the cap because they
            // would have been replaced anyway).
            let already: (i64,) = sqlx::query_as(
                r#"SELECT COUNT(*)
                     FROM media_items
                    WHERE transaction_rid = $1
                      AND deleted_at IS NULL"#,
            )
            .bind(&txn)
            .fetch_one(state.db.reader())
            .await?;
            if already.0 >= crate::handlers::transactions::MAX_ITEMS_PER_TRANSACTION {
                return Err(MediaError::TransactionTooLarge(
                    txn,
                    crate::handlers::transactions::MAX_ITEMS_PER_TRANSACTION,
                ));
            }
            req.transaction_rid.unwrap_or_default()
        }
        _ => String::new(),
    };

    if set.virtual_ {
        return Err(MediaError::BadRequest(
            "virtual media sets do not accept presigned uploads — register items \
             with POST /media-sets/{rid}/virtual-items instead"
                .into(),
        ));
    }

    let new_rid = new_media_item_rid();
    let sha256 = req.sha256.clone().unwrap_or_else(|| {
        // Until the upload completes the client may not know the hash;
        // use the new RID's UUID suffix as a unique placeholder so two
        // concurrent registrations don't collide on storage_uri.
        let placeholder = new_rid
            .strip_prefix(MEDIA_ITEM_RID_PREFIX)
            .unwrap_or(&new_rid)
            .replace('-', "");
        format!("{:0<64}", placeholder.chars().take(64).collect::<String>())
    });

    let key = MediaItemKey::new(media_set_rid, &branch, &sha256);
    let storage_uri_str = storage_uri(state.storage.bucket(), &key);

    // Apply path dedup + insert in one SQL transaction. The
    // `retention_seconds` snapshot is captured from the parent set so
    // the GENERATED `expires_at` column lights up immediately for the
    // partial reaper index — the worker still re-checks against the
    // current parent value to honour PATCH-driven reductions.
    let mut tx = state.db.writer().begin().await?;
    let dedup = soft_delete_previous_at_path(&mut tx, media_set_rid, &branch, &req.path).await?;
    let new_item = NewMediaItem {
        rid: new_rid.clone(),
        media_set_rid: media_set_rid.to_string(),
        branch: branch.clone(),
        transaction_rid,
        path: req.path.clone(),
        mime_type: req.mime_type.clone(),
        size_bytes: req.size_bytes.unwrap_or(0),
        sha256,
        metadata: Value::Object(Default::default()),
        storage_uri: storage_uri_str,
        deduplicated_from: dedup,
    };
    let row: MediaItem = sqlx::query_as(
        // `branch_rid` resolved via subquery so the FK to
        // `media_set_branches` is always consistent with `branch`
        // (no Rust-side md5 hashing duplicates the generated column
        // contract from `0006_branching.sql`).
        r#"INSERT INTO media_items
              (rid, media_set_rid, branch, branch_rid, transaction_rid, path,
               mime_type, size_bytes, sha256, metadata, storage_uri,
               deduplicated_from, retention_seconds)
           VALUES ($1, $2, $3,
                   (SELECT branch_rid FROM media_set_branches
                     WHERE media_set_rid = $2 AND branch_name = $3),
                   $4, $5, $6, $7, $8, $9, $10, $11, $12)
        RETURNING rid, media_set_rid, branch, COALESCE(transaction_rid, '') AS transaction_rid, path, mime_type,
                  size_bytes, sha256, metadata, storage_uri, deduplicated_from,
                  deleted_at, created_at, markings"#,
    )
    .bind(&new_item.rid)
    .bind(&new_item.media_set_rid)
    .bind(&new_item.branch)
    .bind(bind_transaction_rid(&new_item.transaction_rid))
    .bind(&new_item.path)
    .bind(&new_item.mime_type)
    .bind(new_item.size_bytes)
    .bind(&new_item.sha256)
    .bind(&new_item.metadata)
    .bind(&new_item.storage_uri)
    .bind(new_item.deduplicated_from.as_deref())
    .bind(set.retention_seconds)
    .fetch_one(&mut *tx)
    .await?;
    let upload_transaction_rid = if row.transaction_rid.is_empty() {
        None
    } else {
        Some(row.transaction_rid.clone())
    };
    emit_audit(
        &mut tx,
        AuditEvent::MediaItemUploaded {
            resource_rid: row.rid.clone(),
            media_set_rid: row.media_set_rid.clone(),
            project_rid: set.project_rid.clone(),
            markings_at_event: effective_item_markings(&set, &row),
            path: row.path.clone(),
            mime_type: row.mime_type.clone(),
            size_bytes: row.size_bytes,
            sha256: row.sha256.clone(),
            transaction_rid: upload_transaction_rid,
        },
        ctx,
    )
    .await?;
    tx.commit().await?;

    let ttl = Duration::from_secs(req.expires_in_seconds.unwrap_or(state.presign_ttl_seconds));
    let url = state
        .storage
        .presign_upload(&key, &row.mime_type, ttl)
        .await?;

    MEDIA_SET_UPLOADS_TOTAL.inc();
    if row.size_bytes > 0 {
        MEDIA_SET_STORAGE_BYTES.add(row.size_bytes);
        MEDIA_STORAGE_BYTES_LABELLED
            .with_label_values(&[set.schema.as_str(), bool_label(set.virtual_)])
            .add(row.size_bytes);
    }
    Ok((row, url))
}

/// Stable string label for boolean Prometheus dimensions. Avoids the
/// `"true"` / `"True"` ambiguity Prometheus would otherwise produce
/// from `Display`.
fn bool_label(value: bool) -> &'static str {
    if value { "true" } else { "false" }
}

pub async fn presigned_download_op(
    state: &AppState,
    item_rid: &str,
    ttl_secs: Option<u64>,
    ctx: &AuditContext,
) -> MediaResult<(MediaItem, PresignedUrl)> {
    let item = get_item_op(state, item_rid).await?;
    let set = get_media_set_op(state, &item.media_set_rid).await?;
    let ttl = Duration::from_secs(ttl_secs.unwrap_or(state.presign_ttl_seconds));

    let url = if set.virtual_ {
        // Virtual sets: ask `connector-management-service` for the
        // source endpoint + credentials and synthesise a URL pointing
        // at the external system. Bytes never live in Foundry storage.
        resolve_virtual_download_url(state, &set, &item, ttl).await?
    } else {
        let key = MediaItemKey::new(&item.media_set_rid, &item.branch, &item.sha256);
        state.storage.presign_download(&key, ttl).await?
    };

    // Download audit emits AFTER the URL is materialised — emitting
    // before would record a download that the storage backend then
    // refused to issue. The transaction wraps just the outbox INSERT
    // so the emit is still atomic and at-least-once-recoverable.
    let mut tx = state.db.writer().begin().await?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaItemDownloaded {
            resource_rid: item.rid.clone(),
            media_set_rid: item.media_set_rid.clone(),
            project_rid: set.project_rid.clone(),
            markings_at_event: effective_item_markings(&set, &item),
            size_bytes: item.size_bytes,
            ttl_seconds: ttl.as_secs(),
        },
        ctx,
    )
    .await?;
    tx.commit().await?;

    MEDIA_SET_DOWNLOADS_TOTAL.inc();
    Ok((item, url))
}

/// Look up the source descriptor in `connector-management-service` and
/// build an external URL for a virtual media item. Returns
/// [`MediaError::Storage`] when the connector service is not configured
/// or the lookup fails — the HTTP layer surfaces that as 503.
async fn resolve_virtual_download_url(
    state: &AppState,
    set: &crate::models::MediaSet,
    item: &MediaItem,
    ttl: Duration,
) -> MediaResult<PresignedUrl> {
    use chrono::{TimeZone, Utc};

    let base = state.connector_service_url.as_deref().ok_or_else(|| {
        MediaError::UpstreamUnavailable(
            "connector-management-service is not configured; cannot resolve virtual \
                 source endpoint"
                .into(),
        )
    })?;
    let source_rid = set.source_rid.as_deref().ok_or_else(|| {
        MediaError::BadRequest(format!("virtual media set `{}` has no source_rid", set.rid))
    })?;

    let url = format!(
        "{}/sources/{}",
        base.trim_end_matches('/'),
        urlencoding::encode_path(source_rid)
    );
    let resp =
        state.http.get(&url).send().await.map_err(|e| {
            MediaError::UpstreamUnavailable(format!("connector lookup transport: {e}"))
        })?;
    if !resp.status().is_success() {
        return Err(MediaError::UpstreamUnavailable(format!(
            "connector lookup returned HTTP {}",
            resp.status().as_u16()
        )));
    }
    let body: SourceDescriptor = resp
        .json()
        .await
        .map_err(|e| MediaError::UpstreamUnavailable(format!("connector lookup decode: {e}")))?;

    let path_in_source = strip_external_scheme(&item.storage_uri);
    let endpoint = body.endpoint.trim_end_matches('/');
    let expires_at = Utc
        .timestamp_opt(Utc::now().timestamp() + ttl.as_secs() as i64, 0)
        .unwrap();
    let presigned = format!(
        "{endpoint}/{path}?expires={epoch}",
        path = path_in_source.trim_start_matches('/'),
        epoch = expires_at.timestamp()
    );
    Ok(PresignedUrl {
        url: presigned,
        expires_at,
        headers: Vec::new(),
    })
}

/// Strip a leading `s3://bucket/` (or any `<scheme>://<host>/`) prefix
/// from a storage URI so we can re-attach the external endpoint cleanly.
fn strip_external_scheme(uri: &str) -> &str {
    if let Some(rest) = uri.find("://").map(|idx| &uri[idx + 3..]) {
        rest.find('/').map(|i| &rest[i + 1..]).unwrap_or("")
    } else {
        uri
    }
}

mod urlencoding {
    /// Minimal path-safe percent-encoder so RIDs like
    /// `ri.foundry.main.source.<uuid>` survive intact (`.` and digits
    /// are left as-is). Avoids pulling in a whole crate for two slashes.
    pub fn encode_path(s: &str) -> String {
        let mut out = String::with_capacity(s.len());
        for b in s.as_bytes() {
            match b {
                b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' | b':' => {
                    out.push(*b as char);
                }
                _ => out.push_str(&format!("%{b:02X}")),
            }
        }
        out
    }
}

#[derive(Debug, serde::Deserialize)]
struct SourceDescriptor {
    /// Public-facing endpoint (e.g. `https://my-bucket.s3.amazonaws.com`).
    endpoint: String,
}

// ---------------------------------------------------------------------------
// Virtual-item registration
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, serde::Deserialize)]
pub struct RegisterVirtualItemRequest {
    /// Path inside the external source (passed verbatim to consumers as
    /// the storage URI, e.g. `s3://external-bucket/key.png`). Foundry
    /// never copies the bytes.
    pub physical_path: String,
    /// Logical path inside the media set (used for listings + dedup).
    pub item_path: String,
    pub mime_type: Option<String>,
    pub size_bytes: Option<i64>,
    pub branch: Option<String>,
    pub sha256: Option<String>,
}

/// Register a media item that lives in an external source system.
/// The Foundry row only carries metadata + `storage_uri = physical_path`;
/// no bytes are copied. The set's existing path-dedup contract still
/// applies on `(media_set_rid, branch, item_path)`.
pub async fn register_virtual_item_op(
    state: &AppState,
    media_set_rid: &str,
    req: RegisterVirtualItemRequest,
    ctx: &AuditContext,
) -> MediaResult<MediaItem> {
    if req.physical_path.trim().is_empty() || req.item_path.trim().is_empty() {
        return Err(MediaError::BadRequest(
            "physical_path and item_path are required".into(),
        ));
    }
    let set = get_media_set_op(state, media_set_rid).await?;
    if !set.virtual_ {
        return Err(MediaError::BadRequest(
            "register-virtual-item is only valid on virtual media sets".into(),
        ));
    }

    let branch = req.branch.unwrap_or_else(|| "main".to_string());
    let new_rid = new_media_item_rid();
    let sha256 = req.sha256.unwrap_or_default();
    let physical_path = req.physical_path.clone();
    let item_path = req.item_path.clone();

    let mut tx = state.db.writer().begin().await?;
    let dedup = soft_delete_previous_at_path(&mut tx, media_set_rid, &branch, &item_path).await?;
    let row: MediaItem = sqlx::query_as(
        // Same `branch_rid` subquery pattern as `presigned_upload_op`
        // — virtual items live on a branch too, so the FK to
        // `media_set_branches` must be populated on insert.
        r#"INSERT INTO media_items
              (rid, media_set_rid, branch, branch_rid, transaction_rid, path,
               mime_type, size_bytes, sha256, metadata, storage_uri,
               deduplicated_from, retention_seconds)
           VALUES ($1, $2, $3,
                   (SELECT branch_rid FROM media_set_branches
                     WHERE media_set_rid = $2 AND branch_name = $3),
                   NULL, $4, $5, $6, $7, $8::jsonb, $9, $10, $11)
        RETURNING rid, media_set_rid, branch, COALESCE(transaction_rid, '') AS transaction_rid, path, mime_type,
                  size_bytes, sha256, metadata, storage_uri, deduplicated_from,
                  deleted_at, created_at, markings"#,
    )
    .bind(&new_rid)
    .bind(media_set_rid)
    .bind(&branch)
    .bind(&req.item_path)
    .bind(req.mime_type.unwrap_or_default())
    .bind(req.size_bytes.unwrap_or(0))
    .bind(&sha256)
    .bind(serde_json::json!({ "virtual": true }).to_string())
    .bind(&physical_path)
    .bind(dedup.as_deref())
    .bind(set.retention_seconds)
    .fetch_one(&mut *tx)
    .await?;
    emit_audit(
        &mut tx,
        AuditEvent::VirtualMediaItemRegistered {
            resource_rid: row.rid.clone(),
            media_set_rid: row.media_set_rid.clone(),
            project_rid: set.project_rid.clone(),
            markings_at_event: effective_item_markings(&set, &row),
            physical_path,
            item_path,
        },
        ctx,
    )
    .await?;
    tx.commit().await?;

    Ok(row)
}

pub async fn get_item_op(state: &AppState, rid: &str) -> MediaResult<MediaItem> {
    let row: Option<MediaItem> = sqlx::query_as(
        r#"SELECT rid, media_set_rid, branch, COALESCE(transaction_rid, '') AS transaction_rid, path, mime_type,
                  size_bytes, sha256, metadata, storage_uri, deduplicated_from,
                  deleted_at, created_at, markings
             FROM media_items WHERE rid = $1"#,
    )
    .bind(rid)
    .fetch_optional(state.db.reader())
    .await?;
    row.ok_or_else(|| MediaError::MediaItemNotFound(rid.to_string()))
}

pub async fn list_items_op(
    state: &AppState,
    media_set_rid: &str,
    branch: &str,
    prefix: Option<&str>,
    limit: i64,
    cursor: Option<&str>,
) -> MediaResult<Vec<MediaItem>> {
    let limit = limit.clamp(1, 500);
    let rows: Vec<MediaItem> = sqlx::query_as(
        r#"SELECT rid, media_set_rid, branch, COALESCE(transaction_rid, '') AS transaction_rid, path, mime_type,
                  size_bytes, sha256, metadata, storage_uri, deduplicated_from,
                  deleted_at, created_at, markings
             FROM media_items
            WHERE media_set_rid = $1
              AND branch        = $2
              AND deleted_at IS NULL
              AND ($3::text IS NULL OR path LIKE $3 || '%')
              AND ($4::text IS NULL OR path > $4)
         ORDER BY path ASC
            LIMIT $5"#,
    )
    .bind(media_set_rid)
    .bind(branch)
    .bind(prefix)
    .bind(cursor)
    .bind(limit)
    .fetch_all(state.db.reader())
    .await?;
    Ok(rows)
}

pub async fn delete_item_op(state: &AppState, rid: &str, ctx: &AuditContext) -> MediaResult<()> {
    let item = get_item_op(state, rid).await?;
    let parent = get_media_set_op(state, &item.media_set_rid).await?;
    let mut tx = state.db.writer().begin().await?;
    let res = sqlx::query(
        "UPDATE media_items SET deleted_at = NOW() WHERE rid = $1 AND deleted_at IS NULL",
    )
    .bind(rid)
    .execute(&mut *tx)
    .await?;
    let actually_deleted = res.rows_affected() > 0;
    if actually_deleted {
        emit_audit(
            &mut tx,
            AuditEvent::MediaItemDeleted {
                resource_rid: rid.to_string(),
                media_set_rid: item.media_set_rid.clone(),
                project_rid: parent.project_rid.clone(),
                markings_at_event: effective_item_markings(&parent, &item),
                size_bytes: item.size_bytes,
            },
            ctx,
        )
        .await?;
    }
    tx.commit().await?;

    if actually_deleted && item.size_bytes > 0 {
        MEDIA_SET_STORAGE_BYTES.sub(item.size_bytes);
        MEDIA_STORAGE_BYTES_LABELLED
            .with_label_values(&[parent.schema.as_str(), bool_label(parent.virtual_)])
            .sub(item.size_bytes);
    }
    // Best-effort byte cleanup (the metadata row stays for audit).
    if !item.sha256.is_empty() {
        let key = MediaItemKey::new(&item.media_set_rid, &item.branch, &item.sha256);
        let _ = state.storage.delete(&key).await;
    }
    Ok(())
}

// ---------------------------------------------------------------------------
// Axum HTTP handlers
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct ListItemsQuery {
    pub branch: Option<String>,
    pub prefix: Option<String>,
    pub limit: Option<i64>,
    pub cursor: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct DownloadUrlQuery {
    pub expires_in_seconds: Option<u64>,
}

pub async fn presigned_upload(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(media_set_rid): Path<String>,
    Json(body): Json<PresignedUploadRequest>,
) -> Result<(StatusCode, Json<PresignedUrlBody>), MediaErrorResponse> {
    // Pre-check on the parent set: writers need `media_set::manage`
    // (any media item write is a mutation of the set's contents). The
    // freshly-minted item then runs through `media_item::write`.
    let parent = get_media_set_op(&state, &media_set_rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &parent).await?;
    let ctx = from_request(&user.0, &headers);
    let (item, url) = presigned_upload_op(&state, &media_set_rid, body, &ctx).await?;
    check_media_item(&state.engine, &user.0, action_item_write(), &item, &parent).await?;
    Ok((
        StatusCode::CREATED,
        Json(PresignedUrlBody {
            url: url.url,
            expires_at: url.expires_at,
            headers: header_map(&url.headers),
            item: Some(item),
        }),
    ))
}

pub async fn list_items(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(media_set_rid): Path<String>,
    Query(q): Query<ListItemsQuery>,
) -> Result<Json<Vec<MediaItem>>, MediaErrorResponse> {
    let parent = get_media_set_op(&state, &media_set_rid).await?;
    // View on the set is the gate; per-row markings are enforced
    // post-load so the operator gets a fast partial list rather than
    // a hard 403 when a single item is over their clearance.
    check_media_set(&state.engine, &user.0, action_view(), &parent).await?;

    let branch = q.branch.unwrap_or_else(|| "main".to_string());
    let rows = list_items_op(
        &state,
        &media_set_rid,
        &branch,
        q.prefix.as_deref(),
        q.limit.unwrap_or(100),
        q.cursor.as_deref(),
    )
    .await?;
    let mut visible = Vec::with_capacity(rows.len());
    for item in rows {
        if check_media_item(&state.engine, &user.0, action_item_read(), &item, &parent)
            .await
            .is_ok()
        {
            visible.push(item);
        }
    }
    Ok(Json(visible))
}

pub async fn get_item(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<MediaItem>, MediaErrorResponse> {
    let row = get_item_op(&state, &rid).await?;
    let parent = get_media_set_op(&state, &row.media_set_rid).await?;
    check_media_item(&state.engine, &user.0, action_item_read(), &row, &parent).await?;
    Ok(Json(row))
}

pub async fn presigned_download(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Query(q): Query<DownloadUrlQuery>,
) -> Result<Json<PresignedUrlBody>, MediaErrorResponse> {
    let item = get_item_op(&state, &rid).await?;
    let parent = get_media_set_op(&state, &item.media_set_rid).await?;
    // Cedar pre-check — without `media_item::read` no URL is emitted
    // at all (per H3 spec: "sin clearance vigente, URL no se emite").
    check_media_item(&state.engine, &user.0, action_item_read(), &item, &parent).await?;

    let ctx = from_request(&user.0, &headers);
    let (item, url) = presigned_download_op(&state, &rid, q.expires_in_seconds, &ctx).await?;

    // Mint the short-lived JWT claim the edge-gateway validates before
    // letting the GET reach the storage backend. The TTL defaults to
    // 5 minutes; callers that ask for longer presign TTLs cap the
    // claim at the same value.
    let mut effective = parent.markings.clone();
    effective.extend(item.markings.iter().cloned());
    let ttl_secs = q
        .expires_in_seconds
        .map(|s| s.min(state.presign_ttl_seconds) as i64);
    let claim = mint_presign_claim(
        state.presign_secret.as_slice(),
        user.0.sub.to_string(),
        item.rid.clone(),
        effective,
        ttl_secs,
    )?;

    let separator = if url.url.contains('?') { '&' } else { '?' };
    let url_with_claim = format!("{}{}claim={}", url.url, separator, claim);
    Ok(Json(PresignedUrlBody {
        url: url_with_claim,
        expires_at: url.expires_at,
        headers: header_map(&url.headers),
        item: Some(item),
    }))
}

pub async fn delete_item(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
) -> Result<StatusCode, MediaErrorResponse> {
    let item = get_item_op(&state, &rid).await?;
    let parent = get_media_set_op(&state, &item.media_set_rid).await?;
    check_media_item(&state.engine, &user.0, action_item_delete(), &item, &parent).await?;
    let ctx = from_request(&user.0, &headers);
    delete_item_op(&state, &rid, &ctx).await?;
    Ok(StatusCode::NO_CONTENT)
}

pub async fn register_virtual_item(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(media_set_rid): Path<String>,
    Json(body): Json<RegisterVirtualItemRequest>,
) -> Result<(StatusCode, Json<MediaItem>), MediaErrorResponse> {
    let parent = get_media_set_op(&state, &media_set_rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &parent).await?;
    let ctx = from_request(&user.0, &headers);
    let row = register_virtual_item_op(&state, &media_set_rid, body, &ctx).await?;
    check_media_item(&state.engine, &user.0, action_item_write(), &row, &parent).await?;
    Ok((StatusCode::CREATED, Json(row)))
}

// ---------------------------------------------------------------------------
// PATCH /items/{rid}/markings (granular per-item override)
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct PatchItemMarkingsBody {
    /// Replacement set of per-item markings. Empty = inherit only the
    /// parent set's markings (the granular override is removed).
    pub markings: Vec<String>,
}

pub async fn patch_item_markings_op(
    state: &AppState,
    item_rid: &str,
    previous_markings: Vec<String>,
    new_markings: Vec<String>,
    parent: &crate::models::MediaSet,
    ctx: &AuditContext,
) -> MediaResult<MediaItem> {
    let normalised: Vec<String> = {
        let mut seen = std::collections::HashSet::new();
        let mut out = Vec::with_capacity(new_markings.len());
        for raw in &new_markings {
            let lower = raw.trim().to_ascii_lowercase();
            if lower.is_empty() {
                continue;
            }
            if seen.insert(lower.clone()) {
                out.push(lower);
            }
        }
        out.sort();
        out
    };
    let mut tx = state.db.writer().begin().await?;
    let row: MediaItem = sqlx::query_as(
        r#"UPDATE media_items
              SET markings = $2
            WHERE rid = $1
        RETURNING rid, media_set_rid, branch, COALESCE(transaction_rid, '') AS transaction_rid, path, mime_type,
                  size_bytes, sha256, metadata, storage_uri, deduplicated_from,
                  deleted_at, created_at, markings"#,
    )
    .bind(item_rid)
    .bind(&normalised)
    .fetch_one(&mut *tx)
    .await
    .map_err(|err| match err {
        sqlx::Error::RowNotFound => MediaError::MediaItemNotFound(item_rid.to_string()),
        other => MediaError::Database(other),
    })?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaItemMarkingOverridden {
            resource_rid: row.rid.clone(),
            media_set_rid: row.media_set_rid.clone(),
            project_rid: parent.project_rid.clone(),
            markings_at_event: effective_item_markings(parent, &row),
            previous_markings,
        },
        ctx,
    )
    .await?;
    tx.commit().await?;
    Ok(row)
}

pub async fn patch_item_markings(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Json(body): Json<PatchItemMarkingsBody>,
) -> Result<Json<MediaItem>, MediaErrorResponse> {
    let item = get_item_op(&state, &rid).await?;
    let parent = get_media_set_op(&state, &item.media_set_rid).await?;
    // Only operators with `media_set::manage` on the parent can
    // tighten or relax per-item markings — same as the spec for
    // PATCH /media-sets/{rid}/markings.
    check_media_set(&state.engine, &user.0, action_manage(), &parent).await?;
    let ctx = from_request(&user.0, &headers);
    let previous: Vec<String> = item
        .markings
        .iter()
        .map(|m| m.to_ascii_lowercase())
        .collect();
    let updated =
        patch_item_markings_op(&state, &rid, previous, body.markings, &parent, &ctx).await?;
    Ok(Json(updated))
}

fn header_map(headers: &[(String, String)]) -> serde_json::Map<String, Value> {
    let mut out = serde_json::Map::new();
    for (k, v) in headers {
        out.insert(k.clone(), Value::String(v.clone()));
    }
    out
}
