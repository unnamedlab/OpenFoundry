//! Stream branching REST surface (Bloque E1).
//!
//! Exposes 6 endpoints under `/api/v1/streaming/streams/{stream_id}/branches`:
//!
//! | Verb   | Path                                | Purpose                        |
//! |--------|-------------------------------------|--------------------------------|
//! | GET    | `/branches`                         | list                           |
//! | POST   | `/branches`                         | create                         |
//! | GET    | `/branches/{name}`                  | fetch single branch            |
//! | DELETE | `/branches/{name}`                  | hard-delete (only when empty)  |
//! | POST   | `/branches/{name}:merge`            | merge into target branch       |
//! | POST   | `/branches/{name}:archive`          | mark archived & optionally     |
//! |        |                                     | commit cold tier               |
//!
//! Cold-branch materialisation delegates to `dataset-versioning-service`
//! over plain HTTP using `state.http_client` and the URL configured via
//! [`crate::AppState::dataset_service_url`]. Failures only surface in the
//! response message — the local row is always written first.

use auth_middleware::claims::Claims;
use axum::{
    Extension, Json,
    extract::{Path, State},
};
use chrono::Utc;
use sqlx::{Postgres, Transaction, types::Json as SqlJson};
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{
        ServiceResult, bad_request, db_error, forbidden, not_found, streams::emit_audit_event,
    },
    models::{
        ListResponse,
        branch::{
            ArchiveBranchRequest, CreateBranchRequest, MergeBranchRequest, MergeBranchResponse,
            StreamBranch, StreamBranchRow,
        },
    },
    outbox as streaming_outbox,
};

const PERM_BRANCH_WRITE: &str = "streaming:write";

fn can_write(claims: &Claims) -> bool {
    claims.has_any_role(&["admin", "streaming_admin", "data_engineer"])
        || claims.has_permission_key(PERM_BRANCH_WRITE)
}

async fn ensure_stream_exists(db: &sqlx::PgPool, stream_id: Uuid) -> Result<(), sqlx::Error> {
    let exists: bool =
        sqlx::query_scalar("SELECT EXISTS(SELECT 1 FROM streaming_streams WHERE id = $1)")
            .bind(stream_id)
            .fetch_one(db)
            .await?;
    if exists {
        Ok(())
    } else {
        Err(sqlx::Error::RowNotFound)
    }
}

async fn load_branch_by_name(
    db: &sqlx::PgPool,
    stream_id: Uuid,
    name: &str,
) -> Result<StreamBranchRow, sqlx::Error> {
    sqlx::query_as::<_, StreamBranchRow>(
        "SELECT id, stream_id, name, parent_branch_id, status, head_sequence_no,
                dataset_branch_id, description, created_by, created_at, archived_at
           FROM streaming_stream_branches
          WHERE stream_id = $1 AND name = $2",
    )
    .bind(stream_id)
    .bind(name)
    .fetch_one(db)
    .await
}

async fn load_branch_by_name_tx(
    tx: &mut Transaction<'_, Postgres>,
    stream_id: Uuid,
    name: &str,
) -> Result<StreamBranchRow, sqlx::Error> {
    sqlx::query_as::<_, StreamBranchRow>(
        "SELECT id, stream_id, name, parent_branch_id, status, head_sequence_no,
                dataset_branch_id, description, created_by, created_at, archived_at
           FROM streaming_stream_branches
          WHERE stream_id = $1 AND name = $2",
    )
    .bind(stream_id)
    .bind(name)
    .fetch_one(&mut **tx)
    .await
}

pub async fn list_branches(
    State(state): State<AppState>,
    Path(stream_id): Path<Uuid>,
) -> ServiceResult<ListResponse<StreamBranch>> {
    if let Err(sqlx::Error::RowNotFound) = ensure_stream_exists(&state.db, stream_id).await {
        return Err(not_found("stream not found"));
    }
    let rows = sqlx::query_as::<_, StreamBranchRow>(
        "SELECT id, stream_id, name, parent_branch_id, status, head_sequence_no,
                dataset_branch_id, description, created_by, created_at, archived_at
           FROM streaming_stream_branches
          WHERE stream_id = $1
          ORDER BY created_at ASC",
    )
    .bind(stream_id)
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse {
        data: rows.into_iter().map(Into::into).collect(),
    }))
}

pub async fn create_branch(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(stream_id): Path<Uuid>,
    Json(payload): Json<CreateBranchRequest>,
) -> ServiceResult<StreamBranch> {
    if !can_write(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let name = payload.name.trim();
    if name.is_empty() {
        return Err(bad_request("branch name is required"));
    }
    if !name
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_' || c == '/')
    {
        return Err(bad_request(
            "branch name must contain only alphanumerics, '-', '_' or '/'",
        ));
    }
    if let Err(sqlx::Error::RowNotFound) = ensure_stream_exists(&state.db, stream_id).await {
        return Err(not_found("stream not found"));
    }
    // Parent branch (when provided) must exist and belong to the same stream.
    if let Some(parent_id) = payload.parent_branch_id {
        let parent_belongs: bool = sqlx::query_scalar(
            "SELECT EXISTS(SELECT 1 FROM streaming_stream_branches
                            WHERE id = $1 AND stream_id = $2)",
        )
        .bind(parent_id)
        .bind(stream_id)
        .fetch_one(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
        if !parent_belongs {
            return Err(bad_request("parent_branch_id is not part of this stream"));
        }
    }

    let id = Uuid::now_v7();
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query(
        "INSERT INTO streaming_stream_branches (
             id, stream_id, name, parent_branch_id, status, head_sequence_no,
             dataset_branch_id, description, created_by
         ) VALUES ($1, $2, $3, $4, 'active', 0, $5, $6, $7)",
    )
    .bind(id)
    .bind(stream_id)
    .bind(name)
    .bind(payload.parent_branch_id)
    .bind(payload.dataset_branch_id.as_deref())
    .bind(payload.description.unwrap_or_default())
    .bind(&claims.email)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    let row = load_branch_by_name_tx(&mut tx, stream_id, name)
        .await
        .map_err(|cause| db_error(&cause))?;
    let branch: StreamBranch = row.into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::branch_created(&branch))
        .await
        .map_err(|cause| {
            tracing::error!(branch_id = %branch.id, error = %cause, "failed to enqueue outbox event");
            crate::handlers::internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;
    emit_audit_event(
        &claims,
        "streaming.branch.created",
        "streaming_stream_branch",
        branch.id,
        serde_json::json!({ "stream_id": stream_id, "name": branch.name }),
    );
    Ok(Json(branch))
}

pub async fn get_branch(
    State(state): State<AppState>,
    Path((stream_id, name)): Path<(Uuid, String)>,
) -> ServiceResult<StreamBranch> {
    let row = match load_branch_by_name(&state.db, stream_id, &name).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("branch not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    Ok(Json(row.into()))
}

pub async fn delete_branch(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Path((stream_id, name)): Path<(Uuid, String)>,
) -> ServiceResult<serde_json::Value> {
    if !can_write(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    if name == "main" {
        return Err(bad_request("cannot delete the 'main' branch"));
    }
    let row = match load_branch_by_name(&state.db, stream_id, &name).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("branch not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    if row.head_sequence_no > 0 && row.status != "merged" && row.status != "archived" {
        return Err(bad_request(
            "branch has uncommitted history; archive or merge it first",
        ));
    }
    let deleted_at = Utc::now();
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query("DELETE FROM streaming_stream_branches WHERE id = $1")
        .bind(row.id)
        .execute(&mut *tx)
        .await
        .map_err(|cause| db_error(&cause))?;
    let branch: StreamBranch = row.into();
    streaming_outbox::emit(
        &mut tx,
        &streaming_outbox::branch_deleted(&branch, deleted_at, branch.head_sequence_no),
    )
    .await
    .map_err(|cause| {
        tracing::error!(branch_id = %branch.id, error = %cause, "failed to enqueue outbox event");
        crate::handlers::internal_error("failed to enqueue outbox event")
    })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;
    emit_audit_event(
        &claims,
        "streaming.branch.deleted",
        "streaming_stream_branch",
        branch.id,
        serde_json::json!({ "stream_id": stream_id, "name": name }),
    );
    Ok(Json(
        serde_json::json!({ "deleted": true, "id": branch.id }),
    ))
}

pub async fn merge_branch(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Path((stream_id, name)): Path<(Uuid, String)>,
    Json(payload): Json<MergeBranchRequest>,
) -> ServiceResult<MergeBranchResponse> {
    if !can_write(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let target_name = payload
        .target_branch
        .clone()
        .unwrap_or_else(|| "main".to_string());
    if target_name == name {
        return Err(bad_request("target branch must differ from source"));
    }
    let source = match load_branch_by_name(&state.db, stream_id, &name).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("source branch not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let target = match load_branch_by_name(&state.db, stream_id, &target_name).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("target branch not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let merged_seq = source.head_sequence_no.max(target.head_sequence_no);

    let merged_at = Utc::now();
    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query(
        "UPDATE streaming_stream_branches
            SET head_sequence_no = $2
          WHERE id = $1",
    )
    .bind(target.id)
    .bind(merged_seq)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;

    sqlx::query(
        "UPDATE streaming_stream_branches
            SET status = 'merged'
          WHERE id = $1",
    )
    .bind(source.id)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;
    let source_branch: StreamBranch = source.into();
    streaming_outbox::emit(
        &mut tx,
        &streaming_outbox::branch_merged(&source_branch, target.id, merged_seq, merged_at),
    )
    .await
    .map_err(|cause| {
        tracing::error!(branch_id = %source_branch.id, error = %cause, "failed to enqueue outbox event");
        crate::handlers::internal_error("failed to enqueue outbox event")
    })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;

    emit_audit_event(
        &claims,
        "streaming.branch.merged",
        "streaming_stream_branch",
        source_branch.id,
        serde_json::json!({
            "stream_id": stream_id,
            "source": name,
            "target": target_name,
            "merged_sequence_no": merged_seq,
        }),
    );

    Ok(Json(MergeBranchResponse {
        source_branch_id: source_branch.id,
        target_branch_id: target.id,
        merged_sequence_no: merged_seq,
        message: format!("merged '{}' into '{}'", name, target_name),
    }))
}

pub async fn archive_branch(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Path((stream_id, name)): Path<(Uuid, String)>,
    Json(payload): Json<ArchiveBranchRequest>,
) -> ServiceResult<StreamBranch> {
    if !can_write(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let row = match load_branch_by_name(&state.db, stream_id, &name).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("branch not found")),
        Err(cause) => return Err(db_error(&cause)),
    };

    let mut tx = state.db.begin().await.map_err(|cause| db_error(&cause))?;
    sqlx::query(
        "UPDATE streaming_stream_branches
            SET status = 'archived',
                archived_at = now()
          WHERE id = $1",
    )
    .bind(row.id)
    .execute(&mut *tx)
    .await
    .map_err(|cause| db_error(&cause))?;
    let updated = load_branch_by_name_tx(&mut tx, stream_id, &name)
        .await
        .map_err(|cause| db_error(&cause))?;
    let updated_branch: StreamBranch = updated.into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::branch_archived(&updated_branch))
        .await
        .map_err(|cause| {
            tracing::error!(branch_id = %updated_branch.id, error = %cause, "failed to enqueue outbox event");
            crate::handlers::internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|cause| db_error(&cause))?;

    // Best-effort cold-tier commit. We POST the branch metadata to
    // dataset-versioning-service so it can snapshot the cold copy. We
    // never fail the request on HTTP errors \u2014 the local row is already
    // archived; the operator can retry via the dataset-versioning UI.
    if payload.commit_cold {
        if let Some(dataset_branch) = updated_branch.dataset_branch_id.as_deref() {
            let url = format!(
                "{}/api/v1/datasets/{}/branches/{}:commit",
                state.dataset_service_url.trim_end_matches('/'),
                updated_branch.stream_id,
                dataset_branch,
            );
            let body = serde_json::json!({
                "stream_id": stream_id,
                "branch_name": name,
                "head_sequence_no": updated_branch.head_sequence_no,
                "marking": null,
                "archived_at": Utc::now().to_rfc3339(),
            });
            // Suppress lint: best-effort.
            let _ = SqlJson(()); // keep import live in case of future tweaks.
            match state.http_client.post(&url).json(&body).send().await {
                Ok(resp) if resp.status().is_success() => {
                    tracing::info!(branch = %updated_branch.id, "cold tier commit accepted");
                    // Bloque F1: cold archive counter (rows archived).
                    state
                        .metrics
                        .record_stream_rows_archived(&name, updated_branch.head_sequence_no as u64);
                }
                Ok(resp) => {
                    tracing::warn!(
                        branch = %updated_branch.id,
                        status = %resp.status(),
                        "cold tier commit rejected"
                    );
                }
                Err(err) => {
                    tracing::warn!(branch = %updated_branch.id, error = %err, "cold tier commit failed");
                }
            }
        } else {
            tracing::info!(branch = %updated_branch.id, "no dataset_branch_id; skipping cold commit");
        }
    }

    emit_audit_event(
        &claims,
        "streaming.branch.archived",
        "streaming_stream_branch",
        row.id,
        serde_json::json!({
            "stream_id": stream_id,
            "name": name,
            "commit_cold": payload.commit_cold,
        }),
    );
    Ok(Json(updated_branch))
}
