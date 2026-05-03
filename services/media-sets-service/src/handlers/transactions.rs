//! Transactional write batches (Foundry "Advanced media set settings").
//!
//! Open / commit / abort. Only one OPEN transaction can exist per
//! `(media_set, branch)` — enforced both by the application
//! (`MediaError::TransactionTerminal`) and by the partial UNIQUE index
//! `uq_media_set_transactions_one_open_per_branch`.

use std::str::FromStr;

use audit_trail::events::{AuditContext, AuditEvent, emit as emit_audit};
use axum::{
    Json,
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
};
use uuid::Uuid;

use crate::AppState;
use crate::domain::error::{MediaError, MediaResult};
use crate::handlers::audit::from_request;
use crate::handlers::media_sets::{MediaErrorResponse, current_set_markings, get_media_set_op};
use crate::models::{MediaSetTransaction, OpenTransactionBody, TransactionState};

pub const TRANSACTION_RID_PREFIX: &str = "ri.foundry.main.media_transaction.";

pub fn new_transaction_rid() -> String {
    format!("{}{}", TRANSACTION_RID_PREFIX, Uuid::now_v7())
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

pub async fn open_transaction_op(
    state: &AppState,
    media_set_rid: &str,
    branch: &str,
    opened_by: &str,
    ctx: &AuditContext,
) -> MediaResult<MediaSetTransaction> {
    let set = get_media_set_op(state, media_set_rid).await?;
    if set.transaction_policy != "TRANSACTIONAL" {
        return Err(MediaError::Transactionless(media_set_rid.to_string()));
    }

    let rid = new_transaction_rid();
    let mut tx = state.db.writer().begin().await?;
    // The partial unique index turns a concurrent open into a unique
    // violation; surface it as a 409 conflict so callers learn the
    // open-transaction-per-branch invariant.
    let row: MediaSetTransaction = sqlx::query_as(
        r#"INSERT INTO media_set_transactions
              (rid, media_set_rid, branch, state, opened_by)
           VALUES ($1, $2, $3, 'OPEN', $4)
        RETURNING rid, media_set_rid, branch, state, opened_at, closed_at, opened_by"#,
    )
    .bind(&rid)
    .bind(media_set_rid)
    .bind(branch)
    .bind(opened_by)
    .fetch_one(&mut *tx)
    .await
    .map_err(|err| match &err {
        sqlx::Error::Database(db_err) if db_err.is_unique_violation() => {
            MediaError::TransactionTerminal(media_set_rid.to_string(), "OPEN".into())
        }
        _ => MediaError::Database(err),
    })?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetTransactionOpened {
            resource_rid: media_set_rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            transaction_rid: row.rid.clone(),
            branch: row.branch.clone(),
        },
        ctx,
    )
    .await?;
    tx.commit().await?;

    // Bump the per-set in-flight transaction gauge after commit so the
    // value never reflects an uncommitted OPEN.
    crate::metrics::MEDIA_ACTIVE_TRANSACTIONS
        .with_label_values(&[media_set_rid])
        .inc();

    Ok(row)
}

pub async fn close_transaction_op(
    state: &AppState,
    transaction_rid: &str,
    target: TransactionState,
    ctx: &AuditContext,
) -> MediaResult<MediaSetTransaction> {
    if !matches!(target, TransactionState::Committed | TransactionState::Aborted) {
        return Err(MediaError::BadRequest(
            "transaction can only transition to COMMITTED or ABORTED".into(),
        ));
    }

    let current: Option<(String,)> = sqlx::query_as(
        "SELECT state FROM media_set_transactions WHERE rid = $1",
    )
    .bind(transaction_rid)
    .fetch_optional(state.db.reader())
    .await?;
    let current = current.ok_or_else(|| MediaError::TransactionNotFound(transaction_rid.into()))?;
    let current_state = TransactionState::from_str(&current.0)
        .map_err(|e| MediaError::Database(sqlx::Error::Protocol(e)))?;
    if current_state.is_terminal() {
        return Err(MediaError::TransactionTerminal(
            transaction_rid.into(),
            current_state.as_str().into(),
        ));
    }

    let mut tx = state.db.writer().begin().await?;
    let row: MediaSetTransaction = sqlx::query_as(
        r#"UPDATE media_set_transactions
              SET state     = $2,
                  closed_at = NOW()
            WHERE rid = $1
        RETURNING rid, media_set_rid, branch, state, opened_at, closed_at, opened_by"#,
    )
    .bind(transaction_rid)
    .bind(target.as_str())
    .fetch_one(&mut *tx)
    .await?;

    // ABORT semantics: discard staged items so a re-open of the same
    // (set, branch) starts from a clean slate. We hard-delete instead of
    // soft-delete because aborted writes were never readable anyway.
    if matches!(target, TransactionState::Aborted) {
        sqlx::query("DELETE FROM media_items WHERE transaction_rid = $1")
            .bind(transaction_rid)
            .execute(&mut *tx)
            .await?;
    }

    // Set lookup AFTER the row update so the audit event always carries
    // the fresh markings snapshot — a marking change that races with
    // the close still produces a deterministic envelope.
    let set = get_media_set_op(state, &row.media_set_rid).await?;
    let event = match target {
        TransactionState::Committed => AuditEvent::MediaSetTransactionCommitted {
            resource_rid: row.media_set_rid.clone(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            transaction_rid: row.rid.clone(),
            branch: row.branch.clone(),
        },
        TransactionState::Aborted => AuditEvent::MediaSetTransactionAborted {
            resource_rid: row.media_set_rid.clone(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            transaction_rid: row.rid.clone(),
            branch: row.branch.clone(),
        },
        // Unreachable: the early-return above gates non-terminal targets.
        TransactionState::Open => unreachable!("non-terminal target rejected above"),
    };
    emit_audit(&mut tx, event, ctx).await?;
    tx.commit().await?;

    // Decrement the per-set in-flight transaction gauge once the
    // close has hit the WAL. `dec()` on an unset label is a no-op
    // saturated to zero, so a duplicate close (rejected upstream)
    // cannot drive the gauge negative.
    crate::metrics::MEDIA_ACTIVE_TRANSACTIONS
        .with_label_values(&[row.media_set_rid.as_str()])
        .dec();

    Ok(row)
}

// ---------------------------------------------------------------------------
// Axum HTTP handlers
// ---------------------------------------------------------------------------

pub async fn open_transaction(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Json(body): Json<OpenTransactionBody>,
) -> Result<(StatusCode, Json<MediaSetTransaction>), MediaErrorResponse> {
    let branch = body.branch.unwrap_or_else(|| "main".to_string());
    let ctx = from_request(&user.0, &headers);
    let row = open_transaction_op(&state, &rid, &branch, &user.0.sub.to_string(), &ctx).await?;
    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn commit_transaction(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
) -> Result<Json<MediaSetTransaction>, MediaErrorResponse> {
    let ctx = from_request(&user.0, &headers);
    let row = close_transaction_op(&state, &rid, TransactionState::Committed, &ctx).await?;
    Ok(Json(row))
}

pub async fn abort_transaction(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
) -> Result<Json<MediaSetTransaction>, MediaErrorResponse> {
    let ctx = from_request(&user.0, &headers);
    let row = close_transaction_op(&state, &rid, TransactionState::Aborted, &ctx).await?;
    Ok(Json(row))
}
