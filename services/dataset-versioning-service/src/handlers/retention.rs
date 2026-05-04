//! Branch retention HTTP surface.
//!
//! Routes wired in `lib.rs`:
//!
//! ```text
//!   PATCH /v1/datasets/{rid}/branches/{branch}/retention
//!   POST  /v1/datasets/{rid}/branches/{branch}:restore
//!   GET   /v1/datasets/{rid}/branches/{branch}/markings
//! ```
//!
//! Pure thin shims over the SQL surface; the eligibility math lives
//! in `domain::retention` so the worker reuses it.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
};
use chrono::{Duration, Utc};
use serde::Deserialize;
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::domain::branch_events::{self, BranchEnvelope};
use crate::domain::branch_markings::{BranchMarking, BranchMarkingsView, MarkingSource};
use crate::domain::retention::RetentionPolicy;

#[derive(Debug, Deserialize)]
pub struct UpdateRetentionBody {
    pub policy: String,
    #[serde(default)]
    pub ttl_days: Option<i32>,
}

const RESTORE_GRACE_DAYS: i64 = 7;

/// `PATCH /branches/{branch}/retention` — update the retention policy.
pub async fn update_retention(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
    Json(body): Json<UpdateRetentionBody>,
) -> Result<Json<Value>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "branch.retention.update")?;

    let policy = RetentionPolicy::parse(body.policy.trim())
        .ok_or_else(|| bad_request("policy must be one of INHERITED|FOREVER|TTL_DAYS"))?;
    let ttl = if matches!(policy, RetentionPolicy::TtlDays) {
        match body.ttl_days {
            Some(n) if n > 0 => Some(n),
            _ => return Err(bad_request("ttl_days must be > 0 when policy = TTL_DAYS")),
        }
    } else {
        None
    };

    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let row = sqlx::query_as::<_, (Uuid, String, Option<Uuid>, Option<Uuid>, String, String)>(
        r#"UPDATE dataset_branches
              SET retention_policy = $3,
                  retention_ttl_days = $4,
                  updated_at = NOW()
            WHERE dataset_id = $1 AND name = $2 AND deleted_at IS NULL
            RETURNING id, name, parent_branch_id, head_transaction_id, rid, dataset_rid"#,
    )
    .bind(dataset_id)
    .bind(&branch)
    .bind(policy.as_str())
    .bind(ttl)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .ok_or_else(|| not_found("branch not found"))?;

    let (branch_id, branch_name, parent_branch_id, head_id, branch_rid, dataset_rid) = row;
    let mut tx = state.db.begin().await.map_err(internal)?;
    let envelope = BranchEnvelope::new(
        branch_events::EVT_RETENTION_UPDATED,
        &branch_rid,
        &dataset_rid,
        &user.0.sub.to_string(),
    )
    .with_parent_rid(parent_branch_id.map(|id| format!("ri.foundry.main.branch.{id}")))
    .with_head(head_id.map(|id| format!("ri.foundry.main.transaction.{id}")))
    .with_extras(json!({
        "policy": policy.as_str(),
        "ttl_days": ttl,
    }));
    branch_events::emit(&mut tx, &envelope)
        .await
        .map_err(|e| internal(e.to_string()))?;
    tx.commit().await.map_err(internal)?;

    crate::security::emit_audit(
        &user.0.sub,
        "branch.retention.updated",
        &rid,
        json!({
            "branch": branch_name,
            "branch_id": branch_id,
            "policy": policy.as_str(),
            "ttl_days": ttl,
        }),
    );
    Ok(Json(json!({
        "branch": branch_name,
        "policy": policy.as_str(),
        "ttl_days": ttl,
    })))
}

/// `POST /branches/{branch}:restore` — un-archive within the grace
/// window. Branch must be archived; otherwise returns 409.
pub async fn restore_branch(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<Value>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "branch.restore")?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;

    let archived = sqlx::query_as::<_, (Uuid, Option<chrono::DateTime<Utc>>, Option<chrono::DateTime<Utc>>, String, String, Option<Uuid>, Option<Uuid>)>(
        r#"SELECT id, archived_at, archive_grace_until, rid, dataset_rid, parent_branch_id, head_transaction_id
             FROM dataset_branches
            WHERE dataset_id = $1 AND name = $2 AND deleted_at IS NULL"#,
    )
    .bind(dataset_id)
    .bind(&branch)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .ok_or_else(|| not_found("branch not found"))?;

    let (branch_id, archived_at, grace_until, branch_rid, dataset_rid, parent_branch_id, head_id) =
        archived;
    let Some(archived_ts) = archived_at else {
        return Err((
            StatusCode::CONFLICT,
            Json(json!({ "error": "branch is not archived" })),
        ));
    };
    let cutoff = grace_until.unwrap_or(archived_ts + Duration::days(RESTORE_GRACE_DAYS));
    if Utc::now() > cutoff {
        return Err((
            StatusCode::CONFLICT,
            Json(json!({
                "error": "restore grace window has expired",
                "archived_at": archived_ts,
                "grace_until": cutoff,
            })),
        ));
    }

    sqlx::query(
        r#"UPDATE dataset_branches
              SET archived_at = NULL,
                  archive_grace_until = NULL,
                  updated_at = NOW()
            WHERE id = $1"#,
    )
    .bind(branch_id)
    .execute(&state.db)
    .await
    .map_err(internal)?;

    let mut tx = state.db.begin().await.map_err(internal)?;
    let envelope = BranchEnvelope::new(
        branch_events::EVT_RESTORED,
        &branch_rid,
        &dataset_rid,
        &user.0.sub.to_string(),
    )
    .with_parent_rid(parent_branch_id.map(|id| format!("ri.foundry.main.branch.{id}")))
    .with_head(head_id.map(|id| format!("ri.foundry.main.transaction.{id}")))
    .with_extras(json!({ "archived_at": archived_ts }));
    branch_events::emit(&mut tx, &envelope)
        .await
        .map_err(|e| internal(e.to_string()))?;
    tx.commit().await.map_err(internal)?;

    crate::security::emit_audit(
        &user.0.sub,
        "branch.restored",
        &rid,
        json!({ "branch": branch, "archived_at": archived_ts }),
    );

    Ok(Json(json!({
        "branch": branch,
        "restored_at": Utc::now(),
        "previously_archived_at": archived_ts,
    })))
}

/// `GET /branches/{branch}/markings` — effective + explicit + inherited
/// projection of `branch_markings_snapshot`.
pub async fn get_branch_markings(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<BranchMarkingsView>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let branch_id = sqlx::query_scalar::<_, Uuid>(
        r#"SELECT id FROM dataset_branches
            WHERE dataset_id = $1 AND name = $2 AND deleted_at IS NULL"#,
    )
    .bind(dataset_id)
    .bind(&branch)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .ok_or_else(|| not_found("branch not found"))?;

    let rows: Vec<(Uuid, String)> = sqlx::query_as(
        r#"SELECT marking_id, source FROM branch_markings_snapshot
            WHERE branch_id = $1
            ORDER BY marking_id"#,
    )
    .bind(branch_id)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;

    let snapshot: Vec<BranchMarking> = rows
        .into_iter()
        .map(|(marking_id, source)| BranchMarking {
            branch_id,
            marking_id,
            source: match source.as_str() {
                "PARENT" => MarkingSource::Parent,
                _ => MarkingSource::Explicit,
            },
        })
        .collect();
    Ok(Json(BranchMarkingsView::from_rows(&snapshot)))
}

// ── helpers (duplicated from `foundry.rs` to avoid pulling pub on its
// internals just for the new module). ──

async fn resolve_dataset_id(
    state: &AppState,
    rid: &str,
) -> Result<Uuid, (StatusCode, Json<Value>)> {
    if let Ok(id) = Uuid::parse_str(rid) {
        return Ok(id);
    }
    let row = sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?;
    row.ok_or_else(|| not_found("dataset not found"))
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
}

fn not_found(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::NOT_FOUND, Json(json!({ "error": msg })))
}

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "dataset-versioning-service: retention handler error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}
