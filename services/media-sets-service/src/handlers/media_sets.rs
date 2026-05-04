//! Media-set CRUD: create, list (filtered by `project_rid`), get, delete.
//!
//! The HTTP handlers are thin wrappers over `*_op` functions so the gRPC
//! service in `crate::grpc` can re-use the same SQL without duplicating
//! it (and so unit tests can hit the operations directly).

use audit_trail::events::{AuditContext, AuditEvent, emit as emit_audit};
use auth_middleware::Claims;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::AppState;
use crate::domain::cedar::{action_delete_set, action_manage, action_view, check_media_set};
use crate::domain::error::{MediaError, MediaResult};
use crate::handlers::audit::from_request;
use crate::models::{CreateMediaSetRequest, MediaSet, MediaSetSchema};

/// Foundry RID prefix for media sets (`ri.foundry.main.media_set.<uuid>`).
pub const MEDIA_SET_RID_PREFIX: &str = "ri.foundry.main.media_set.";

/// Generate a fresh media-set RID using the same UUID-v7 convention as
/// `core_models::DatasetRid`.
pub fn new_media_set_rid() -> String {
    format!("{}{}", MEDIA_SET_RID_PREFIX, Uuid::now_v7())
}

// ---------------------------------------------------------------------------
// Operations (shared with gRPC)
// ---------------------------------------------------------------------------

pub async fn create_media_set_op(
    state: &AppState,
    req: CreateMediaSetRequest,
    created_by: &str,
    ctx: &AuditContext,
) -> MediaResult<MediaSet> {
    if req.name.trim().is_empty() {
        return Err(MediaError::BadRequest("name must not be empty".into()));
    }
    if req.project_rid.trim().is_empty() {
        return Err(MediaError::BadRequest(
            "project_rid must not be empty".into(),
        ));
    }
    if req.virtual_ && req.source_rid.as_deref().unwrap_or_default().is_empty() {
        return Err(MediaError::BadRequest(
            "virtual media sets require a source_rid".into(),
        ));
    }

    let rid = new_media_set_rid();

    // Single Postgres transaction: insert + branch boot + outbox audit
    // emit. Either all three land or none do (ADR-0022).
    let mut tx = state.db.writer().begin().await?;
    let row: MediaSet = sqlx::query_as(
        r#"INSERT INTO media_sets
              (rid, project_rid, name, schema, allowed_mime_types,
               transaction_policy, retention_seconds, virtual,
               source_rid, markings, created_by)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING rid, project_rid, name, schema, allowed_mime_types,
                  transaction_policy, retention_seconds, virtual,
                  source_rid, markings, created_at, created_by"#,
    )
    .bind(&rid)
    .bind(&req.project_rid)
    .bind(&req.name)
    .bind(req.schema.as_str())
    .bind(&req.allowed_mime_types)
    .bind(req.transaction_policy.as_str())
    .bind(req.retention_seconds)
    .bind(req.virtual_)
    .bind(req.source_rid.as_deref())
    .bind(&req.markings)
    .bind(created_by)
    .fetch_one(&mut *tx)
    .await?;

    // Boot the implicit `main` branch so transactions / items can be
    // attached without the caller having to call a separate API. Per
    // H4 (`0006_branching.sql`), `main` is always a root branch
    // (`parent_branch_rid IS NULL`) and starts with no head pointer.
    sqlx::query(
        r#"INSERT INTO media_set_branches
              (media_set_rid, branch_name, parent_branch_rid,
               head_transaction_rid, created_by)
           VALUES ($1, 'main', NULL, NULL, $2)
           ON CONFLICT DO NOTHING"#,
    )
    .bind(&row.rid)
    .bind(created_by)
    .execute(&mut *tx)
    .await?;

    emit_audit(
        &mut tx,
        AuditEvent::MediaSetCreated {
            resource_rid: row.rid.clone(),
            project_rid: row.project_rid.clone(),
            markings_at_event: current_set_markings(&row),
            name: row.name.clone(),
            schema: row.schema.clone(),
            transaction_policy: row.transaction_policy.clone(),
            virtual_: row.virtual_,
        },
        ctx,
    )
    .await?;

    tx.commit().await?;
    Ok(row)
}

pub async fn list_media_sets_op(
    state: &AppState,
    project_rid: Option<&str>,
    limit: i64,
    offset: i64,
) -> MediaResult<Vec<MediaSet>> {
    let limit = limit.clamp(1, 200);
    let rows: Vec<MediaSet> = sqlx::query_as(
        r#"SELECT rid, project_rid, name, schema, allowed_mime_types,
                  transaction_policy, retention_seconds, virtual,
                  source_rid, markings, created_at, created_by
             FROM media_sets
            WHERE ($1::text IS NULL OR project_rid = $1)
         ORDER BY created_at DESC
            LIMIT $2 OFFSET $3"#,
    )
    .bind(project_rid)
    .bind(limit)
    .bind(offset.max(0))
    .fetch_all(state.db.reader())
    .await?;
    Ok(rows)
}

pub async fn get_media_set_op(state: &AppState, rid: &str) -> MediaResult<MediaSet> {
    let row: Option<MediaSet> = sqlx::query_as(
        r#"SELECT rid, project_rid, name, schema, allowed_mime_types,
                  transaction_policy, retention_seconds, virtual,
                  source_rid, markings, created_at, created_by
             FROM media_sets
            WHERE rid = $1"#,
    )
    .bind(rid)
    .fetch_optional(state.db.reader())
    .await?;
    row.ok_or_else(|| MediaError::MediaSetNotFound(rid.to_string()))
}

pub async fn delete_media_set_op(
    state: &AppState,
    rid: &str,
    set: &MediaSet,
    ctx: &AuditContext,
) -> MediaResult<()> {
    let mut tx = state.db.writer().begin().await?;
    let res = sqlx::query("DELETE FROM media_sets WHERE rid = $1")
        .bind(rid)
        .execute(&mut *tx)
        .await?;
    if res.rows_affected() == 0 {
        return Err(MediaError::MediaSetNotFound(rid.to_string()));
    }
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetDeleted {
            resource_rid: rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(set),
        },
        ctx,
    )
    .await?;
    tx.commit().await?;
    Ok(())
}

/// Hard floor on the per-set retention window in seconds. Reductions
/// below this floor on transactional sets are rejected because Foundry
/// guarantees that an OPEN transaction's items remain visible at least
/// until the transaction is sealed; a too-aggressive window would let
/// the reaper delete bytes mid-flight.
///
/// Follow-up H?: wire this against the published platform SLO once
/// `services/sds-service` defines `media-set.retention.min_seconds`.
/// Until then we apply a conservative 60-second floor purely as a
/// guardrail.
pub const TRANSACTIONAL_RETENTION_FLOOR_SECONDS: i64 = 60;

/// Update `media_sets.retention_seconds` and run a one-shot reaper on
/// the affected set so reductions take effect immediately
/// ("Advanced media set settings.md" → *Retention policies*).
pub async fn patch_retention_op(
    state: &AppState,
    rid: &str,
    new_retention_seconds: i64,
    ctx: &AuditContext,
) -> MediaResult<MediaSet> {
    if new_retention_seconds < 0 {
        return Err(MediaError::BadRequest(
            "retention_seconds must be >= 0 (0 = retain forever)".into(),
        ));
    }
    let set = get_media_set_op(state, rid).await?;
    if set.transaction_policy == "TRANSACTIONAL"
        && new_retention_seconds > 0
        && new_retention_seconds < TRANSACTIONAL_RETENTION_FLOOR_SECONDS
        && new_retention_seconds < set.retention_seconds
    {
        return Err(MediaError::BadRequest(format!(
            "transactional media sets cannot be reduced below the {}s SLO floor",
            TRANSACTIONAL_RETENTION_FLOOR_SECONDS
        )));
    }

    let previous_retention_seconds = set.retention_seconds;
    let mut tx = state.db.writer().begin().await?;
    let updated: MediaSet = sqlx::query_as(
        r#"UPDATE media_sets
              SET retention_seconds = $2
            WHERE rid = $1
        RETURNING rid, project_rid, name, schema, allowed_mime_types,
                  transaction_policy, retention_seconds, virtual,
                  source_rid, markings, created_at, created_by"#,
    )
    .bind(rid)
    .bind(new_retention_seconds)
    .fetch_one(&mut *tx)
    .await?;

    emit_audit(
        &mut tx,
        AuditEvent::MediaSetRetentionChanged {
            resource_rid: updated.rid.clone(),
            project_rid: updated.project_rid.clone(),
            markings_at_event: current_set_markings(&updated),
            previous_retention_seconds,
            new_retention_seconds,
        },
        ctx,
    )
    .await?;
    tx.commit().await?;

    // "Reduction is immediate": run the reaper synchronously on this
    // set so the next read no longer surfaces newly-expired items.
    // Expansion is a no-op here — the reaper UPDATE only flips
    // `deleted_at` from NULL to NOW(), and rows that are already
    // expired stay expired regardless of the new (larger) window.
    let expired = crate::domain::retention::reap_media_set(state.db.writer(), rid).await?;
    if !expired.is_empty() {
        crate::domain::retention::drop_bytes(state.storage.as_ref(), &expired).await;
        crate::domain::retention::emit_audit(&expired);
    }

    Ok(updated)
}

#[derive(Debug, Deserialize)]
pub struct PatchRetentionBody {
    pub retention_seconds: i64,
}

/// Resolve the live MediaSet schema, used by the items handler to
/// validate / surface the schema enum without re-querying.
pub async fn require_media_set(state: &AppState, rid: &str) -> MediaResult<MediaSet> {
    get_media_set_op(state, rid).await
}

pub fn schema_for(set: &MediaSet) -> MediaSetSchema {
    set.schema.parse().unwrap_or(MediaSetSchema::Document)
}

// ---------------------------------------------------------------------------
// Axum HTTP handlers
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct ListQuery {
    pub project_rid: Option<String>,
    pub limit: Option<i64>,
    pub offset: Option<i64>,
}

pub async fn create_media_set(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Json(body): Json<CreateMediaSetRequest>,
) -> Result<(StatusCode, Json<MediaSet>), MediaErrorResponse> {
    let ctx = from_request(&user.0, &headers);
    let row = create_media_set_op(&state, body, &user.0.sub.to_string(), &ctx).await?;
    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn list_media_sets(
    State(state): State<AppState>,
    _user: auth_middleware::layer::AuthUser,
    Query(q): Query<ListQuery>,
) -> Result<Json<Vec<MediaSet>>, MediaErrorResponse> {
    let rows = list_media_sets_op(
        &state,
        q.project_rid.as_deref(),
        q.limit.unwrap_or(50),
        q.offset.unwrap_or(0),
    )
    .await?;
    Ok(Json(rows))
}

pub async fn get_media_set(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<MediaSet>, MediaErrorResponse> {
    let row = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_view(), &row).await?;
    Ok(Json(row))
}

pub async fn delete_media_set(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
) -> Result<StatusCode, MediaErrorResponse> {
    let row = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_delete_set(), &row).await?;
    let ctx = from_request(&user.0, &headers);
    delete_media_set_op(&state, &rid, &row, &ctx).await?;
    Ok(StatusCode::NO_CONTENT)
}

pub async fn patch_retention(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Json(body): Json<PatchRetentionBody>,
) -> Result<Json<MediaSet>, MediaErrorResponse> {
    let row = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &row).await?;
    let ctx = from_request(&user.0, &headers);
    let row = patch_retention_op(&state, &rid, body.retention_seconds, &ctx).await?;
    Ok(Json(row))
}

// ---------------------------------------------------------------------------
// Markings — PATCH + dry-run preview (H3)
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct PatchMarkingsBody {
    /// Replacement set of markings (case-insensitive). The handler
    /// normalises to lowercase before persisting so the Cedar entity
    /// hydration produces stable `Marking::"<name>"` UIDs.
    pub markings: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct MarkingsPreviewResponse {
    /// New marking set the operator is about to apply, normalised.
    pub markings: Vec<String>,
    /// Markings currently in effect — included so the UI dry-run can
    /// surface the diff without a second round-trip.
    pub current_markings: Vec<String>,
    /// Markings that would be added (in `markings` but not `current`).
    pub added: Vec<String>,
    /// Markings that would be removed (in `current` but not `markings`).
    pub removed: Vec<String>,
    /// Number of users that will lose access. Wired against the
    /// identity-federation user catalog in a future task; for H3 we
    /// always return 0 (the engine itself surfaces denials at request
    /// time).
    pub users_losing_access: u32,
}

fn normalise_markings(input: &[String]) -> Vec<String> {
    let mut seen = std::collections::HashSet::new();
    let mut out = Vec::with_capacity(input.len());
    for raw in input {
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
}

pub async fn patch_markings_op(
    state: &AppState,
    rid: &str,
    previous_markings: Vec<String>,
    new_markings: Vec<String>,
    ctx: &AuditContext,
) -> MediaResult<MediaSet> {
    let normalised = normalise_markings(&new_markings);
    let mut tx = state.db.writer().begin().await?;
    let row: MediaSet = sqlx::query_as(
        r#"UPDATE media_sets
              SET markings = $2
            WHERE rid = $1
        RETURNING rid, project_rid, name, schema, allowed_mime_types,
                  transaction_policy, retention_seconds, virtual,
                  source_rid, markings, created_at, created_by"#,
    )
    .bind(rid)
    .bind(&normalised)
    .fetch_one(&mut *tx)
    .await
    .map_err(|err| match err {
        sqlx::Error::RowNotFound => MediaError::MediaSetNotFound(rid.to_string()),
        other => MediaError::Database(other),
    })?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetMarkingsChanged {
            resource_rid: row.rid.clone(),
            project_rid: row.project_rid.clone(),
            // Snapshot AFTER the change so SIEM rules can rebuild the
            // clearance envelope at audit time without re-querying.
            markings_at_event: current_set_markings(&row),
            previous_markings,
        },
        ctx,
    )
    .await?;
    tx.commit().await?;
    Ok(row)
}

pub async fn patch_markings(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Json(body): Json<PatchMarkingsBody>,
) -> Result<Json<MediaSet>, MediaErrorResponse> {
    let row = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &row).await?;
    let ctx = from_request(&user.0, &headers);
    let previous = current_set_markings(&row);
    let updated = patch_markings_op(&state, &rid, previous, body.markings, &ctx).await?;
    Ok(Json(updated))
}

pub async fn preview_markings(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
    Json(body): Json<PatchMarkingsBody>,
) -> Result<Json<MarkingsPreviewResponse>, MediaErrorResponse> {
    let row = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &row).await?;

    let current: Vec<String> = row
        .markings
        .iter()
        .map(|m| m.to_ascii_lowercase())
        .collect();
    let next = normalise_markings(&body.markings);

    let current_set: std::collections::HashSet<&String> = current.iter().collect();
    let next_set: std::collections::HashSet<&String> = next.iter().collect();
    let added: Vec<String> = next
        .iter()
        .filter(|m| !current_set.contains(m))
        .cloned()
        .collect();
    let removed: Vec<String> = current
        .iter()
        .filter(|m| !next_set.contains(m))
        .cloned()
        .collect();

    Ok(Json(MarkingsPreviewResponse {
        markings: next,
        current_markings: current,
        added,
        removed,
        // Follow-up H4: cross-reference the user catalog to estimate how
        // many active sessions will lose access. The Cedar engine
        // already enforces the new envelope at request time, so this
        // is a UX hint, not a gate.
        users_losing_access: 0,
    }))
}

/// Re-export so the gRPC service (and tests) can grab the canonical
/// list without depending on the HTTP handler module structure.
pub fn current_set_markings(set: &MediaSet) -> Vec<String> {
    set.markings
        .iter()
        .map(|m| m.to_ascii_lowercase())
        .collect()
}

#[allow(dead_code)]
fn _ensure_claims_used(_c: &Claims) {}

// ---------------------------------------------------------------------------
// Error → response conversion
// ---------------------------------------------------------------------------

/// Newtype wrapper so we can `impl IntoResponse for MediaError` without
/// orphan-rule issues (MediaError lives in the `domain::error` module).
pub struct MediaErrorResponse(pub MediaError);

impl From<MediaError> for MediaErrorResponse {
    fn from(value: MediaError) -> Self {
        Self(value)
    }
}

impl IntoResponse for MediaErrorResponse {
    fn into_response(self) -> axum::response::Response {
        let (status, msg) = match &self.0 {
            MediaError::MediaSetNotFound(_)
            | MediaError::MediaItemNotFound(_)
            | MediaError::TransactionNotFound(_)
            | MediaError::BranchNotFound(_) => (StatusCode::NOT_FOUND, self.0.to_string()),
            MediaError::Transactionless(_) | MediaError::TransactionTerminal(_, _) => {
                (StatusCode::CONFLICT, self.0.to_string())
            }
            MediaError::TransactionlessRejectsReset(_)
            | MediaError::TransactionlessRejectsReplace(_)
            | MediaError::TransactionTooLarge(_, _) => {
                (StatusCode::UNPROCESSABLE_ENTITY, self.0.to_string())
            }
            MediaError::MergeConflict(paths) => {
                let body = serde_json::json!({
                    "error": "merge conflict",
                    "code": "MEDIA_SET_BRANCH_MERGE_CONFLICT",
                    "conflict_paths": paths,
                });
                return (StatusCode::CONFLICT, Json(body)).into_response();
            }
            MediaError::BadRequest(_) => (StatusCode::BAD_REQUEST, self.0.to_string()),
            MediaError::Forbidden(msg) => {
                tracing::info!(reason = %msg, "cedar denial");
                (StatusCode::FORBIDDEN, msg.clone())
            }
            MediaError::Authz(msg) => {
                tracing::error!(error = %msg, "authz internal error");
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "authz internal error".into(),
                )
            }
            MediaError::Storage(msg) => {
                tracing::warn!(error = %msg, "storage backend error");
                (StatusCode::BAD_GATEWAY, "storage backend error".to_string())
            }
            MediaError::UpstreamUnavailable(msg) => {
                tracing::warn!(error = %msg, "upstream service unavailable");
                (StatusCode::SERVICE_UNAVAILABLE, msg.clone())
            }
            MediaError::Database(err) => {
                tracing::error!(error = %err, "database error");
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "database error".to_string(),
                )
            }
            MediaError::Outbox(err) => {
                tracing::error!(error = %err, "audit outbox error");
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "audit outbox error".to_string(),
                )
            }
        };
        // Stable error codes for the H4 invariants the front-end and
        // pipelines branch on. `null` for variants that don't have a
        // dedicated code yet — clients fall back to the HTTP status.
        let code = match &self.0 {
            MediaError::TransactionTooLarge(_, _) => Some("MEDIA_SET_TRANSACTION_TOO_LARGE"),
            MediaError::TransactionlessRejectsReset(_) => {
                Some("MEDIA_SET_TRANSACTIONLESS_REJECTS_RESET")
            }
            MediaError::TransactionlessRejectsReplace(_) => {
                Some("MEDIA_SET_TRANSACTIONLESS_REJECTS_REPLACE")
            }
            _ => None,
        };
        let body = match code {
            Some(c) => serde_json::json!({ "error": msg, "code": c }),
            None => serde_json::json!({ "error": msg }),
        };
        (status, Json(body)).into_response()
    }
}
