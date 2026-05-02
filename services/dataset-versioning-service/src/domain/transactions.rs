use sqlx::{PgPool, Postgres, Transaction};
use uuid::Uuid;

use crate::models::transaction::DatasetTransaction;

pub struct TransactionRecord {
    pub view_id: Option<Uuid>,
    pub operation: String,
    pub branch_name: Option<String>,
    pub summary: String,
    pub metadata: serde_json::Value,
}

pub async fn record_committed_transaction(
    tx: &mut Transaction<'_, Postgres>,
    dataset_id: Uuid,
    record: TransactionRecord,
) -> Result<DatasetTransaction, sqlx::Error> {
    sqlx::query_as::<_, DatasetTransaction>(
        r#"INSERT INTO dataset_transactions (
               id, dataset_id, view_id, operation, branch_name, status, summary, metadata, committed_at
           )
           VALUES ($1, $2, $3, $4, $5, 'committed', $6, $7::jsonb, NOW())
           RETURNING *"#,
    )
    .bind(Uuid::now_v7())
    .bind(dataset_id)
    .bind(record.view_id)
    .bind(record.operation)
    .bind(record.branch_name)
    .bind(record.summary)
    .bind(record.metadata)
    .fetch_one(&mut **tx)
    .await
}

// ────────────────────────────────────────────────────────────────────────────
// Commit / abort orchestration (used by `handlers::foundry::transaction_action`).
//
// The full Foundry invariants — APPEND cannot rewrite existing files,
// DELETE only carries REMOVE ops, SNAPSHOT cannot carry REMOVE ops — are
// represented in the [`CommitError`] taxonomy so the HTTP layer can map
// each one to a precise status code. The current implementation provides
// the canonical state transition (OPEN → COMMITTED / ABORTED) and leaves
// the kind-specific validators as future work; they're already plumbed
// through the public API surface.
// ────────────────────────────────────────────────────────────────────────────

/// Failure modes for `commit_transaction` / `abort_transaction`.
///
/// Variants intentionally mirror the HTTP errors returned by
/// `transaction_action`: each one is exhaustively matched in
/// `handlers::foundry::map_commit_error`.
#[derive(Debug, thiserror::Error)]
pub enum CommitError {
    #[error("transaction not found")]
    NotFound,
    #[error("transaction is not OPEN (current state: {current})")]
    NotOpen { current: String },
    #[error("APPEND would modify {count} existing file(s)")]
    AppendModifiesExisting { count: usize, paths: Vec<String> },
    #[error("DELETE may only carry REMOVE ops ({count} non-REMOVE found)")]
    DeleteWithWriteOps { count: usize, paths: Vec<String> },
    #[error("SNAPSHOT cannot stage REMOVE ops ({count} found)")]
    SnapshotWithRemoveOps { count: usize, paths: Vec<String> },
    #[error("unknown transaction kind: {kind}")]
    UnknownKind { kind: String },
    #[error("database error: {0}")]
    Database(String),
}

impl From<sqlx::Error> for CommitError {
    fn from(e: sqlx::Error) -> Self {
        CommitError::Database(e.to_string())
    }
}

/// Transition an OPEN transaction to COMMITTED.
///
/// Kind-specific validation (APPEND / DELETE / SNAPSHOT invariants) is
/// declared in [`CommitError`] and will be enforced once the staged-file
/// table is wired in (tracked alongside T1.4). For now the function only
/// guards the OPEN → COMMITTED transition; non-OPEN transactions return
/// [`CommitError::NotOpen`] and unknown transactions return
/// [`CommitError::NotFound`].
pub async fn commit_transaction(db: &PgPool, txn_id: Uuid) -> Result<(), CommitError> {
    let updated = sqlx::query(
        r#"UPDATE dataset_transactions
              SET status = 'committed', committed_at = NOW()
            WHERE id = $1 AND status = 'open'"#,
    )
    .bind(txn_id)
    .execute(db)
    .await?;
    if updated.rows_affected() == 1 {
        return Ok(());
    }
    classify_transition_failure(db, txn_id).await
}

/// Transition an OPEN transaction to ABORTED. Same semantics as
/// `commit_transaction`.
pub async fn abort_transaction(db: &PgPool, txn_id: Uuid) -> Result<(), CommitError> {
    let updated = sqlx::query(
        r#"UPDATE dataset_transactions
              SET status = 'aborted', aborted_at = NOW()
            WHERE id = $1 AND status = 'open'"#,
    )
    .bind(txn_id)
    .execute(db)
    .await?;
    if updated.rows_affected() == 1 {
        return Ok(());
    }
    classify_transition_failure(db, txn_id).await
}

async fn classify_transition_failure(db: &PgPool, txn_id: Uuid) -> Result<(), CommitError> {
    let row =
        sqlx::query_scalar::<_, String>("SELECT status FROM dataset_transactions WHERE id = $1")
            .bind(txn_id)
            .fetch_optional(db)
            .await?;
    match row {
        None => Err(CommitError::NotFound),
        Some(state) => Err(CommitError::NotOpen { current: state }),
    }
}
