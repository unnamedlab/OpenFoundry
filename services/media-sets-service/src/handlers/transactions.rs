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
use crate::domain::cedar::{action_view, check_media_set};
use crate::handlers::media_sets::{MediaErrorResponse, current_set_markings, get_media_set_op};
use crate::models::{
    MediaSetTransaction, OpenTransactionBody, TransactionHistoryEntry, TransactionState, WriteMode,
};

/// Foundry per-transaction item cap. Quoted directly from
/// `Advanced media set settings.md` ("A maximum of 10,000 items can be
/// written in a single transaction.")
pub const MAX_ITEMS_PER_TRANSACTION: i64 = 10_000;

/// `GET /media-sets/{rid}/transactions` — paginated history feed.
///
/// Returns one [`TransactionHistoryEntry`] per transaction on the
/// set, ordered by most-recent-first, including the per-transaction
/// item diff so the History tab can render added / modified / deleted
/// counts without a follow-up roundtrip. The query is intentionally a
/// single LEFT JOIN + GROUP BY on `media_items` so a media set with
/// thousands of transactions still resolves in one shot.
pub async fn list_transactions_op(
    state: &AppState,
    media_set_rid: &str,
) -> MediaResult<Vec<TransactionHistoryEntry>> {
    let rows: Vec<TransactionHistoryEntry> = sqlx::query_as(
        r#"
        WITH stats AS (
            SELECT
                t.rid,
                COUNT(*) FILTER (
                    WHERE i.transaction_rid = t.rid
                      AND i.deduplicated_from IS NULL
                ) AS items_added,
                COUNT(*) FILTER (
                    WHERE i.transaction_rid = t.rid
                      AND i.deduplicated_from IS NOT NULL
                ) AS items_modified,
                COUNT(*) FILTER (
                    WHERE i.deleted_at IS NOT NULL
                      AND i.media_set_rid = t.media_set_rid
                      AND i.branch = t.branch
                      AND i.transaction_rid IS DISTINCT FROM t.rid
                      -- Soft-deletes that overlap this transaction's
                      -- close window. Approximation: anything closed
                      -- in this transaction's lifetime that wasn't
                      -- written by it. Good enough for the History
                      -- view; the audit trail carries the precise
                      -- per-item deletion timestamp.
                      AND i.deleted_at >= t.opened_at
                      AND (t.closed_at IS NULL OR i.deleted_at <= t.closed_at)
                ) AS items_deleted
              FROM media_set_transactions t
         LEFT JOIN media_items i
                ON i.media_set_rid = t.media_set_rid
               AND (i.transaction_rid = t.rid OR i.deleted_at IS NOT NULL)
             WHERE t.media_set_rid = $1
          GROUP BY t.rid
        )
        SELECT t.rid, t.media_set_rid, t.branch, t.state, t.write_mode,
               t.opened_at, t.closed_at, t.opened_by,
               COALESCE(s.items_added, 0)    AS items_added,
               COALESCE(s.items_modified, 0) AS items_modified,
               COALESCE(s.items_deleted, 0)  AS items_deleted
          FROM media_set_transactions t
     LEFT JOIN stats s ON s.rid = t.rid
         WHERE t.media_set_rid = $1
      ORDER BY t.opened_at DESC
        "#,
    )
    .bind(media_set_rid)
    .fetch_all(state.db.reader())
    .await?;
    Ok(rows)
}

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
    write_mode: WriteMode,
    ctx: &AuditContext,
) -> MediaResult<MediaSetTransaction> {
    let set = get_media_set_op(state, media_set_rid).await?;
    if set.transaction_policy != "TRANSACTIONAL" {
        // Transactionless sets reject the entire transaction surface,
        // not just REPLACE — uploads write directly with NULL
        // `transaction_rid`.
        return Err(MediaError::Transactionless(media_set_rid.to_string()));
    }
    // Defence in depth: even though only TRANSACTIONAL sets reach
    // here, REPLACE on a transactionless set is the canonical
    // Foundry rejection — keep the explicit code path so future
    // refactors (e.g. exposing a transactionless "synthetic
    // transaction" wrapper) cannot accidentally accept REPLACE.
    if matches!(write_mode, WriteMode::Replace)
        && set.transaction_policy != "TRANSACTIONAL"
    {
        return Err(MediaError::TransactionlessRejectsReplace(
            media_set_rid.to_string(),
        ));
    }

    let rid = new_transaction_rid();
    let mut tx = state.db.writer().begin().await?;
    // The partial unique index turns a concurrent open into a unique
    // violation; surface it as a 409 conflict so callers learn the
    // open-transaction-per-branch invariant.
    let row: MediaSetTransaction = sqlx::query_as(
        r#"INSERT INTO media_set_transactions
              (rid, media_set_rid, branch, state, opened_by, write_mode)
           VALUES ($1, $2, $3, 'OPEN', $4, $5)
        RETURNING rid, media_set_rid, branch, state, opened_at, closed_at, opened_by"#,
    )
    .bind(&rid)
    .bind(media_set_rid)
    .bind(branch)
    .bind(opened_by)
    .bind(write_mode.as_str())
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
    if !matches!(
        target,
        TransactionState::Committed | TransactionState::Aborted
    ) {
        return Err(MediaError::BadRequest(
            "transaction can only transition to COMMITTED or ABORTED".into(),
        ));
    }

    let current: Option<(String, String)> = sqlx::query_as(
        "SELECT state, write_mode FROM media_set_transactions WHERE rid = $1",
    )
    .bind(transaction_rid)
    .fetch_optional(state.db.reader())
    .await?;
    let (current_state_str, write_mode_str) =
        current.ok_or_else(|| MediaError::TransactionNotFound(transaction_rid.into()))?;
    let current_state = TransactionState::from_str(&current_state_str)
        .map_err(|e| MediaError::Database(sqlx::Error::Protocol(e)))?;
    if current_state.is_terminal() {
        return Err(MediaError::TransactionTerminal(
            transaction_rid.into(),
            current_state.as_str().into(),
        ));
    }
    let opened_with_replace = write_mode_str == "REPLACE";

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

    // COMMIT in REPLACE mode: surface only the items written in the
    // transaction. Every prior live item on the same branch (that
    // wasn't written by *this* transaction) is soft-deleted in a
    // single UPDATE so the path-dedup index stays consistent.
    // (Foundry "Incremental media sets" → "set_write_mode('replace')".)
    if matches!(target, TransactionState::Committed) && opened_with_replace {
        sqlx::query(
            r#"UPDATE media_items
                  SET deleted_at = NOW()
                WHERE media_set_rid = $1
                  AND branch = $2
                  AND deleted_at IS NULL
                  AND COALESCE(transaction_rid, '') <> $3"#,
        )
        .bind(&row.media_set_rid)
        .bind(&row.branch)
        .bind(transaction_rid)
        .execute(&mut *tx)
        .await?;
    }

    // COMMIT advances the branch's head pointer; ABORT is a no-op
    // for the head (the prior commit, if any, is still authoritative).
    if matches!(target, TransactionState::Committed) {
        sqlx::query(
            r#"UPDATE media_set_branches
                  SET head_transaction_rid = $1
                WHERE media_set_rid = $2 AND branch_name = $3"#,
        )
        .bind(transaction_rid)
        .bind(&row.media_set_rid)
        .bind(&row.branch)
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
    let write_mode = body.write_mode.unwrap_or_default();
    let ctx = from_request(&user.0, &headers);
    let row = open_transaction_op(
        &state,
        &rid,
        &branch,
        &user.0.sub.to_string(),
        write_mode,
        &ctx,
    )
    .await?;
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

pub async fn list_transactions(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<Vec<TransactionHistoryEntry>>, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_view(), &set).await?;
    Ok(Json(list_transactions_op(&state, &rid).await?))
}
