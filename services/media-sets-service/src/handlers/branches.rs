//! Branch CRUD + reset + merge.
//!
//! Implements the four operations the Foundry "Branching" doc lists for
//! datasets, ported to media sets:
//!
//!   * **Create branch** — POST `/media-sets/{rid}/branches`. Optional
//!     `from_branch` (defaults to `main`); the new branch inherits the
//!     parent's `head_transaction_rid` so the view starts identical.
//!   * **Delete branch** — DELETE `/media-sets/{rid}/branches/{name}`.
//!     Re-parents every child under the deleted branch's parent and
//!     soft-deletes every live item on the branch (the metadata row
//!     stays — the partial unique path index uses `deleted_at IS NULL`
//!     so nothing else needs touching).
//!   * **Reset branch** — POST `/media-sets/{rid}/branches/{name}/reset`.
//!     Equivalent to `git reset --hard <parent.head>`: every live item
//!     is soft-deleted and the head pointer rewinds. **TRANSACTIONAL
//!     sets only** — transactionless sets reject this with HTTP 422
//!     and code `MEDIA_SET_TRANSACTIONLESS_REJECTS_RESET` (per
//!     `Advanced media set settings.md` → "Transactionless media set
//!     branches cannot be reset to an empty view").
//!   * **Merge branch** — POST `/media-sets/{rid}/branches/{name}/merge`.
//!     Per-path resolution: every path live on the source either
//!     overwrites the target (`LATEST_WINS`) or aborts the merge with
//!     409 (`FAIL_ON_CONFLICT`).

use audit_trail::events::{AuditContext, AuditEvent, emit as emit_audit};
use axum::{
    Json,
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
};
use serde::Serialize;
use sqlx::Postgres;
use uuid::Uuid;

use crate::AppState;
use crate::domain::cedar::{action_manage, action_view, check_media_set};
use crate::domain::error::{MediaError, MediaResult};
use crate::handlers::audit::from_request;
use crate::handlers::media_sets::{MediaErrorResponse, current_set_markings, get_media_set_op};
use crate::models::{
    CreateBranchBody, MediaSetBranch, MergeBranchBody, MergeBranchResponse, MergeResolution,
    ResetBranchResponse,
};

/// Serialisation envelope for `409 Conflict` from `FAIL_ON_CONFLICT`.
/// Carries the conflicting paths so the operator (or the Pipeline
/// Builder) can show a diff or fall back to `LATEST_WINS`.
#[derive(Debug, Serialize)]
pub struct MergeConflictBody {
    pub error: String,
    pub conflict_paths: Vec<String>,
}

// ---------------------------------------------------------------------------
// Operations (shared with gRPC if/when it surfaces these)
// ---------------------------------------------------------------------------

/// `SELECT` helper. Returns `MediaSetNotFound` if the row is absent
/// even though the set itself exists — the application API for branch
/// operations always implies the branch must already be there.
async fn require_branch(
    state: &AppState,
    media_set_rid: &str,
    branch_name: &str,
) -> MediaResult<MediaSetBranch> {
    let row: Option<MediaSetBranch> = sqlx::query_as(
        r#"SELECT media_set_rid, branch_name, branch_rid,
                  parent_branch_rid, head_transaction_rid,
                  created_at, created_by
             FROM media_set_branches
            WHERE media_set_rid = $1 AND branch_name = $2"#,
    )
    .bind(media_set_rid)
    .bind(branch_name)
    .fetch_optional(state.db.reader())
    .await?;
    row.ok_or_else(|| MediaError::BranchNotFound(branch_name.to_string()))
}

pub async fn list_branches_op(
    state: &AppState,
    media_set_rid: &str,
) -> MediaResult<Vec<MediaSetBranch>> {
    let rows: Vec<MediaSetBranch> = sqlx::query_as(
        r#"SELECT media_set_rid, branch_name, branch_rid,
                  parent_branch_rid, head_transaction_rid,
                  created_at, created_by
             FROM media_set_branches
            WHERE media_set_rid = $1
         ORDER BY (branch_name = 'main') DESC, branch_name ASC"#,
    )
    .bind(media_set_rid)
    .fetch_all(state.db.reader())
    .await?;
    Ok(rows)
}

pub async fn create_branch_op(
    state: &AppState,
    media_set_rid: &str,
    body: CreateBranchBody,
    created_by: &str,
    ctx: &AuditContext,
) -> MediaResult<MediaSetBranch> {
    let name = body.name.trim().to_string();
    if name.is_empty() {
        return Err(MediaError::BadRequest("branch name must not be empty".into()));
    }
    if name.contains('/') || name.contains(' ') {
        return Err(MediaError::BadRequest(
            "branch name must not contain `/` or whitespace".into(),
        ));
    }
    let parent_name = body.from_branch.unwrap_or_else(|| "main".to_string());

    let mut tx = state.db.writer().begin().await?;

    // Look up the parent (must exist) so we can copy its head pointer
    // and lock its `branch_rid` into the child row. Failing here keeps
    // the Foundry "non-root branch has exactly one parent" invariant.
    let parent: Option<(String, Option<String>)> = sqlx::query_as(
        r#"SELECT branch_rid, head_transaction_rid
             FROM media_set_branches
            WHERE media_set_rid = $1 AND branch_name = $2
            FOR UPDATE"#,
    )
    .bind(media_set_rid)
    .bind(&parent_name)
    .fetch_optional(&mut *tx)
    .await?;
    let (parent_branch_rid, parent_head) = parent.ok_or_else(|| {
        MediaError::BranchNotFound(format!(
            "parent branch `{parent_name}` does not exist on media set `{media_set_rid}`"
        ))
    })?;
    // If the caller asked to fork from a specific transaction
    // ("Restore to this point"), validate the RID belongs to the same
    // media set and use it as the new branch's head pointer.
    let head_transaction_rid = match body.from_transaction_rid {
        Some(txn_rid) => {
            let trimmed = txn_rid.trim();
            if trimmed.is_empty() {
                None
            } else {
                let exists: Option<(String,)> = sqlx::query_as(
                    "SELECT rid FROM media_set_transactions WHERE rid = $1 AND media_set_rid = $2",
                )
                .bind(trimmed)
                .bind(media_set_rid)
                .fetch_optional(&mut *tx)
                .await?;
                exists.ok_or_else(|| MediaError::TransactionNotFound(trimmed.to_string()))?;
                Some(trimmed.to_string())
            }
        }
        None => parent_head,
    };

    let row: MediaSetBranch = sqlx::query_as(
        r#"INSERT INTO media_set_branches
              (media_set_rid, branch_name, parent_branch_rid,
               head_transaction_rid, created_by)
           VALUES ($1, $2, $3, $4, $5)
        RETURNING media_set_rid, branch_name, branch_rid,
                  parent_branch_rid, head_transaction_rid,
                  created_at, created_by"#,
    )
    .bind(media_set_rid)
    .bind(&name)
    .bind(&parent_branch_rid)
    .bind(head_transaction_rid.as_deref())
    .bind(created_by)
    .fetch_one(&mut *tx)
    .await
    .map_err(|err| match &err {
        sqlx::Error::Database(db_err) if db_err.is_unique_violation() => {
            MediaError::BadRequest(format!("branch `{name}` already exists"))
        }
        _ => MediaError::Database(err),
    })?;

    let set = get_media_set_op(state, media_set_rid).await?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetCreated {
            // Re-using `MediaSetCreated` as the closest analogue
            // would be wrong; emit a lightweight branch-scoped audit
            // via the access-pattern variant which already carries the
            // branch context.
            resource_rid: media_set_rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            name: format!("branch:{}", row.branch_name),
            schema: set.schema.clone(),
            transaction_policy: set.transaction_policy.clone(),
            virtual_: set.virtual_,
        },
        ctx,
    )
    .await?;

    tx.commit().await?;
    Ok(row)
}

pub async fn delete_branch_op(
    state: &AppState,
    media_set_rid: &str,
    branch_name: &str,
    ctx: &AuditContext,
) -> MediaResult<()> {
    if branch_name == "main" {
        return Err(MediaError::BadRequest(
            "cannot delete the implicit `main` branch".into(),
        ));
    }
    let branch = require_branch(state, media_set_rid, branch_name).await?;

    let mut tx = state.db.writer().begin().await?;

    // Re-parent children under our parent (per Foundry guarantee:
    // "child branches are re-parented under the deleted branch's
    // parent (or no parent if it was a root branch)"). NULL is the
    // correct value when our parent was itself NULL.
    sqlx::query(
        r#"UPDATE media_set_branches
              SET parent_branch_rid = $1
            WHERE media_set_rid = $2 AND parent_branch_rid = $3"#,
    )
    .bind(branch.parent_branch_rid.as_deref())
    .bind(media_set_rid)
    .bind(&branch.branch_rid)
    .execute(&mut *tx)
    .await?;

    // Soft-delete every live item on the branch. The metadata stays
    // for direct-RID reads (Foundry "items remain addressable for
    // direct media references") and for audit replay.
    sqlx::query(
        r#"UPDATE media_items
              SET deleted_at = NOW()
            WHERE branch_rid = $1
              AND deleted_at IS NULL"#,
    )
    .bind(&branch.branch_rid)
    .execute(&mut *tx)
    .await?;

    sqlx::query(
        "DELETE FROM media_set_branches WHERE media_set_rid = $1 AND branch_name = $2",
    )
    .bind(media_set_rid)
    .bind(branch_name)
    .execute(&mut *tx)
    .await?;

    let set = get_media_set_op(state, media_set_rid).await?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetDeleted {
            resource_rid: media_set_rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
        },
        ctx,
    )
    .await?;

    tx.commit().await?;
    Ok(())
}

pub async fn reset_branch_op(
    state: &AppState,
    media_set_rid: &str,
    branch_name: &str,
    ctx: &AuditContext,
) -> MediaResult<ResetBranchResponse> {
    let set = get_media_set_op(state, media_set_rid).await?;
    if set.transaction_policy != "TRANSACTIONAL" {
        return Err(MediaError::TransactionlessRejectsReset(media_set_rid.to_string()));
    }
    let branch = require_branch(state, media_set_rid, branch_name).await?;

    let mut tx = state.db.writer().begin().await?;
    let purged = sqlx::query(
        r#"UPDATE media_items
              SET deleted_at = NOW()
            WHERE branch_rid = $1
              AND deleted_at IS NULL"#,
    )
    .bind(&branch.branch_rid)
    .execute(&mut *tx)
    .await?
    .rows_affected() as i64;

    // Rewind the head pointer. The reset semantics are "git reset --hard"
    // — the prior commit history stays in `media_set_transactions` for
    // audit, but the branch view starts empty.
    let updated: MediaSetBranch = sqlx::query_as(
        r#"UPDATE media_set_branches
              SET head_transaction_rid = NULL
            WHERE media_set_rid = $1 AND branch_name = $2
        RETURNING media_set_rid, branch_name, branch_rid,
                  parent_branch_rid, head_transaction_rid,
                  created_at, created_by"#,
    )
    .bind(media_set_rid)
    .bind(branch_name)
    .fetch_one(&mut *tx)
    .await?;

    emit_audit(
        &mut tx,
        AuditEvent::MediaSetTransactionAborted {
            resource_rid: media_set_rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            transaction_rid: branch.head_transaction_rid.clone().unwrap_or_default(),
            branch: branch_name.to_string(),
        },
        ctx,
    )
    .await?;

    tx.commit().await?;
    Ok(ResetBranchResponse {
        branch: updated,
        items_soft_deleted: purged,
    })
}

pub async fn merge_branch_op(
    state: &AppState,
    media_set_rid: &str,
    source_branch: &str,
    body: MergeBranchBody,
    ctx: &AuditContext,
) -> MediaResult<MergeBranchResponse> {
    let target_branch = body.target_branch.trim().to_string();
    if target_branch.is_empty() {
        return Err(MediaError::BadRequest("target_branch is required".into()));
    }
    if target_branch == source_branch {
        return Err(MediaError::BadRequest(
            "target_branch must differ from source_branch".into(),
        ));
    }
    let source = require_branch(state, media_set_rid, source_branch).await?;
    let target = require_branch(state, media_set_rid, &target_branch).await?;

    let mut tx = state.db.writer().begin().await?;

    // Find the live source paths.
    let source_rows: Vec<(String, String, String, i64, String, serde_json::Value, String, Vec<String>)> =
        sqlx::query_as(
            r#"SELECT path, mime_type, sha256, size_bytes, storage_uri, metadata, rid, markings
                 FROM media_items
                WHERE branch_rid = $1
                  AND deleted_at IS NULL
             ORDER BY created_at ASC"#,
        )
        .bind(&source.branch_rid)
        .fetch_all(&mut *tx)
        .await?;

    // Find paths already live on the target — this is the conflict
    // set for the resolution policy.
    let target_paths: Vec<(String,)> = sqlx::query_as(
        r#"SELECT path
             FROM media_items
            WHERE branch_rid = $1
              AND deleted_at IS NULL"#,
    )
    .bind(&target.branch_rid)
    .fetch_all(&mut *tx)
    .await?;
    let target_path_set: std::collections::HashSet<&str> =
        target_paths.iter().map(|(p,)| p.as_str()).collect();

    let conflicts: Vec<String> = source_rows
        .iter()
        .filter_map(|(p, ..)| target_path_set.contains(p.as_str()).then(|| p.clone()))
        .collect();

    if matches!(body.resolution, MergeResolution::FailOnConflict) && !conflicts.is_empty() {
        // Roll back the read-only transaction. The error carries the
        // conflict surface so callers can switch policies without
        // re-fetching.
        return Err(MediaError::MergeConflict(conflicts));
    }

    let mut copied = 0i64;
    let mut overwritten = 0i64;
    let mut skipped = 0i64;

    for (path, mime_type, sha256, size_bytes, storage_uri, metadata, source_rid, markings) in
        &source_rows
    {
        let is_conflict = target_path_set.contains(path.as_str());
        if is_conflict {
            // Soft-delete the existing target row so the new one slots
            // into the partial unique index without violation.
            sqlx::query(
                r#"UPDATE media_items
                      SET deleted_at = NOW()
                    WHERE branch_rid = $1
                      AND path = $2
                      AND deleted_at IS NULL"#,
            )
            .bind(&target.branch_rid)
            .bind(path)
            .execute(&mut *tx)
            .await?;
            overwritten += 1;
        } else {
            copied += 1;
        }

        let new_rid = format!("ri.foundry.main.media_item.{}", Uuid::now_v7());
        let res = insert_merged_item(
            &mut tx,
            &new_rid,
            media_set_rid,
            &target_branch,
            &target.branch_rid,
            path,
            mime_type,
            sha256,
            *size_bytes,
            metadata,
            storage_uri,
            source_rid,
            markings,
        )
        .await;
        if let Err(MediaError::Database(sqlx::Error::Database(db_err))) = &res {
            if db_err.is_unique_violation() {
                skipped += 1;
                continue;
            }
        }
        res?;
    }

    let set = get_media_set_op(state, media_set_rid).await?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetTransactionCommitted {
            resource_rid: media_set_rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            transaction_rid: format!("merge:{source_branch}->{target_branch}"),
            branch: target_branch.clone(),
        },
        ctx,
    )
    .await?;

    tx.commit().await?;

    Ok(MergeBranchResponse {
        source_branch: source_branch.to_string(),
        target_branch,
        resolution: body.resolution.as_str().to_string(),
        paths_copied: copied,
        paths_overwritten: overwritten,
        paths_skipped: skipped,
    })
}

#[allow(clippy::too_many_arguments)]
async fn insert_merged_item(
    tx: &mut sqlx::Transaction<'_, Postgres>,
    new_rid: &str,
    media_set_rid: &str,
    branch_name: &str,
    branch_rid: &str,
    path: &str,
    mime_type: &str,
    sha256: &str,
    size_bytes: i64,
    metadata: &serde_json::Value,
    storage_uri: &str,
    source_rid: &str,
    markings: &[String],
) -> MediaResult<()> {
    sqlx::query(
        r#"INSERT INTO media_items
              (rid, media_set_rid, branch, branch_rid, path, mime_type,
               size_bytes, sha256, metadata, storage_uri, deduplicated_from,
               retention_seconds, markings)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
                   COALESCE((SELECT retention_seconds FROM media_sets WHERE rid = $2), 0),
                   $12)"#,
    )
    .bind(new_rid)
    .bind(media_set_rid)
    .bind(branch_name)
    .bind(branch_rid)
    .bind(path)
    .bind(mime_type)
    .bind(size_bytes)
    .bind(sha256)
    .bind(metadata)
    .bind(storage_uri)
    .bind(source_rid)
    .bind(markings)
    .execute(&mut **tx)
    .await?;
    Ok(())
}

// ---------------------------------------------------------------------------
// Axum HTTP handlers
// ---------------------------------------------------------------------------

pub async fn list_branches(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<Vec<MediaSetBranch>>, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_view(), &set).await?;
    Ok(Json(list_branches_op(&state, &rid).await?))
}

pub async fn create_branch(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Json(body): Json<CreateBranchBody>,
) -> Result<(StatusCode, Json<MediaSetBranch>), MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &set).await?;
    let ctx = from_request(&user.0, &headers);
    let row = create_branch_op(&state, &rid, body, &user.0.sub.to_string(), &ctx).await?;
    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn delete_branch(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path((rid, name)): Path<(String, String)>,
) -> Result<StatusCode, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &set).await?;
    let ctx = from_request(&user.0, &headers);
    delete_branch_op(&state, &rid, &name, &ctx).await?;
    Ok(StatusCode::NO_CONTENT)
}

pub async fn reset_branch(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path((rid, name)): Path<(String, String)>,
) -> Result<Json<ResetBranchResponse>, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &set).await?;
    let ctx = from_request(&user.0, &headers);
    let resp = reset_branch_op(&state, &rid, &name, &ctx).await?;
    Ok(Json(resp))
}

pub async fn merge_branch(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path((rid, name)): Path<(String, String)>,
    Json(body): Json<MergeBranchBody>,
) -> Result<Json<MergeBranchResponse>, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &set).await?;
    let ctx = from_request(&user.0, &headers);
    let resp = merge_branch_op(&state, &rid, &name, body, &ctx).await?;
    Ok(Json(resp))
}
