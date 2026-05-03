use std::collections::BTreeMap;

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Postgres, Row, Transaction};
use uuid::Uuid;

use crate::models::transaction::DatasetTransaction;
use crate::storage::runtime::{NewCommittedTransaction, insert_committed_transaction_tx};

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
    insert_committed_transaction_tx(
        tx,
        NewCommittedTransaction {
            id: Uuid::now_v7(),
            dataset_id,
            view_id: record.view_id,
            operation: record.operation,
            branch_name: record.branch_name,
            summary: record.summary,
            metadata: record.metadata,
        },
    )
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
    #[error("branch {branch} already has an OPEN transaction")]
    ConcurrentOpenTransaction { branch: String },
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

#[derive(Debug, Clone)]
struct TxnRow {
    id: Uuid,
    dataset_id: Uuid,
    branch_id: Uuid,
    branch_name: String,
    tx_type: String,
    status: String,
    summary: String,
}

#[derive(Debug, Clone)]
struct FileRow {
    logical_path: String,
    size_bytes: i64,
    op: String,
}

#[derive(Debug, Clone)]
struct ViewFile {
    size_bytes: i64,
}

/// Transition an OPEN transaction to COMMITTED.
///
/// Commit is a single SQL transaction: it locks the transaction row,
/// validates staged-file invariants, advances branch HEAD, creates a
/// dataset version, updates dataset counters for the active branch, and
/// marks previous branch transactions historical after a SNAPSHOT.
pub async fn commit_transaction(db: &PgPool, txn_id: Uuid) -> Result<(), CommitError> {
    let mut tx = db.begin().await?;
    let row = load_txn_for_update(&mut tx, txn_id)
        .await?
        .ok_or(CommitError::NotFound)?;
    if row.status != "OPEN" {
        return Err(CommitError::NotOpen {
            current: row.status,
        });
    }

    let staged = load_file_rows(&mut tx, row.id).await?;
    let before = compute_committed_view_tx(&mut tx, row.branch_id, None).await?;
    validate_commit(&row.tx_type, &staged, &before)?;
    let after = apply_transaction(before, &row.tx_type, &staged)?;
    let file_count = after.len() as i32;
    let size_bytes = after.values().map(|file| file.size_bytes).sum::<i64>();

    sqlx::query(
        r#"UPDATE dataset_transactions
              SET status = 'COMMITTED',
                  committed_at = NOW(),
                  metadata = metadata || $2::jsonb
            WHERE id = $1"#,
    )
    .bind(row.id)
    .bind(serde_json::json!({
        "file_count": file_count,
        "size_bytes": size_bytes,
    }))
    .execute(&mut *tx)
    .await?;

    if row.tx_type == "SNAPSHOT" {
        sqlx::query(
            r#"UPDATE dataset_transactions
                  SET metadata = metadata || '{"historical": true}'::jsonb
                WHERE branch_id = $1
                  AND id <> $2
                  AND status = 'COMMITTED'"#,
        )
        .bind(row.branch_id)
        .bind(row.id)
        .execute(&mut *tx)
        .await?;
    }

    sqlx::query(
        r#"UPDATE dataset_branches
              SET head_transaction_id = $2,
                  updated_at = NOW()
            WHERE id = $1"#,
    )
    .bind(row.branch_id)
    .bind(row.id)
    .execute(&mut *tx)
    .await?;

    let next_version = next_version_for_update_tx(&mut tx, row.dataset_id).await?;
    let storage_path =
        sqlx::query_scalar::<_, String>("SELECT storage_path FROM datasets WHERE id = $1")
            .bind(row.dataset_id)
            .fetch_one(&mut *tx)
            .await?;
    sqlx::query(
        r#"INSERT INTO dataset_versions
           (id, dataset_id, version, message, size_bytes, row_count, storage_path, transaction_id)
           VALUES ($1, $2, $3, $4, $5, 0, $6, $7)
           ON CONFLICT (dataset_id, version) DO NOTHING"#,
    )
    .bind(Uuid::now_v7())
    .bind(row.dataset_id)
    .bind(next_version)
    .bind(&row.summary)
    .bind(size_bytes)
    .bind(format!("{storage_path}/v{next_version}"))
    .bind(row.id)
    .execute(&mut *tx)
    .await?;

    sqlx::query(
        r#"UPDATE dataset_branches
              SET version = $3,
                  updated_at = NOW()
            WHERE dataset_id = $1 AND name = $2"#,
    )
    .bind(row.dataset_id)
    .bind(&row.branch_name)
    .bind(next_version)
    .execute(&mut *tx)
    .await?;

    sqlx::query(
        r#"UPDATE datasets
              SET current_version = CASE WHEN active_branch = $2 THEN $3 ELSE current_version END,
                  size_bytes = CASE WHEN active_branch = $2 THEN $4 ELSE size_bytes END,
                  metadata = CASE
                    WHEN $5 = 'UPDATE' THEN metadata || '{"incremental_friendly": false}'::jsonb
                    ELSE metadata
                  END,
                  updated_at = NOW()
            WHERE id = $1"#,
    )
    .bind(row.dataset_id)
    .bind(&row.branch_name)
    .bind(next_version)
    .bind(size_bytes)
    .bind(&row.tx_type)
    .execute(&mut *tx)
    .await?;

    tx.commit().await?;
    Ok(())
}

/// Transition an OPEN transaction to ABORTED. Same semantics as
/// `commit_transaction`.
pub async fn abort_transaction(db: &PgPool, txn_id: Uuid) -> Result<(), CommitError> {
    let mut tx = db.begin().await?;
    let row = load_txn_for_update(&mut tx, txn_id)
        .await?
        .ok_or(CommitError::NotFound)?;
    match row.status.as_str() {
        "OPEN" => {
            sqlx::query(
                r#"UPDATE dataset_transactions
                      SET status = 'ABORTED',
                          aborted_at = COALESCE(aborted_at, NOW())
                    WHERE id = $1"#,
            )
            .bind(row.id)
            .execute(&mut *tx)
            .await?;
            tx.commit().await?;
            Ok(())
        }
        "ABORTED" => {
            tx.commit().await?;
            Ok(())
        }
        other => Err(CommitError::NotOpen {
            current: other.to_string(),
        }),
    }
}

async fn load_txn_for_update(
    tx: &mut Transaction<'_, Postgres>,
    txn_id: Uuid,
) -> Result<Option<TxnRow>, sqlx::Error> {
    let row = sqlx::query(
        r#"SELECT id, dataset_id, branch_id, branch_name, tx_type, status, summary
             FROM dataset_transactions
            WHERE id = $1
            FOR UPDATE"#,
    )
    .bind(txn_id)
    .fetch_optional(&mut **tx)
    .await?;

    Ok(row.map(|row| TxnRow {
        id: row.get("id"),
        dataset_id: row.get("dataset_id"),
        branch_id: row.get("branch_id"),
        branch_name: row.get("branch_name"),
        tx_type: row.get("tx_type"),
        status: row.get("status"),
        summary: row.get("summary"),
    }))
}

async fn next_version_for_update_tx(
    tx: &mut Transaction<'_, Postgres>,
    dataset_id: Uuid,
) -> Result<i32, sqlx::Error> {
    let current = sqlx::query_scalar::<_, i32>(
        "SELECT current_version FROM datasets WHERE id = $1 FOR UPDATE",
    )
    .bind(dataset_id)
    .fetch_one(&mut **tx)
    .await?;
    let max_version = sqlx::query_scalar::<_, Option<i32>>(
        "SELECT MAX(version) FROM dataset_versions WHERE dataset_id = $1",
    )
    .bind(dataset_id)
    .fetch_one(&mut **tx)
    .await?;
    Ok(max_version.unwrap_or(current.saturating_sub(1)) + 1)
}

async fn load_file_rows(
    tx: &mut Transaction<'_, Postgres>,
    txn_id: Uuid,
) -> Result<Vec<FileRow>, sqlx::Error> {
    let rows = sqlx::query(
        r#"SELECT logical_path, size_bytes, op
             FROM dataset_transaction_files
            WHERE transaction_id = $1
            ORDER BY logical_path ASC"#,
    )
    .bind(txn_id)
    .fetch_all(&mut **tx)
    .await?;
    Ok(rows
        .into_iter()
        .map(|row| FileRow {
            logical_path: row.get("logical_path"),
            size_bytes: row.get("size_bytes"),
            op: row.get("op"),
        })
        .collect())
}

async fn compute_committed_view_tx(
    tx: &mut Transaction<'_, Postgres>,
    branch_id: Uuid,
    at: Option<DateTime<Utc>>,
) -> Result<BTreeMap<String, ViewFile>, sqlx::Error> {
    let txn_rows = sqlx::query(
        r#"SELECT id, tx_type
             FROM dataset_transactions
            WHERE branch_id = $1
              AND status = 'COMMITTED'
              AND ($2::timestamptz IS NULL OR COALESCE(committed_at, started_at) <= $2)
            ORDER BY COALESCE(committed_at, started_at) ASC, started_at ASC"#,
    )
    .bind(branch_id)
    .bind(at)
    .fetch_all(&mut **tx)
    .await?;

    let mut view = BTreeMap::new();
    for row in txn_rows {
        let txn_id: Uuid = row.get("id");
        let tx_type: String = row.get("tx_type");
        let files = load_file_rows(tx, txn_id).await?;
        view = apply_transaction(view, &tx_type, &files)
            .map_err(|err| sqlx::Error::Protocol(err.to_string()))?;
    }
    Ok(view)
}

fn validate_commit(
    tx_type: &str,
    staged: &[FileRow],
    current_view: &BTreeMap<String, ViewFile>,
) -> Result<(), CommitError> {
    match tx_type {
        "SNAPSHOT" => {
            let paths = staged
                .iter()
                .filter(|file| file.op == "REMOVE")
                .map(|file| file.logical_path.clone())
                .collect::<Vec<_>>();
            if !paths.is_empty() {
                return Err(CommitError::SnapshotWithRemoveOps {
                    count: paths.len(),
                    paths,
                });
            }
        }
        "APPEND" => {
            let paths = staged
                .iter()
                .filter(|file| file.op != "ADD" || current_view.contains_key(&file.logical_path))
                .map(|file| file.logical_path.clone())
                .collect::<Vec<_>>();
            if !paths.is_empty() {
                return Err(CommitError::AppendModifiesExisting {
                    count: paths.len(),
                    paths,
                });
            }
        }
        "UPDATE" => {}
        "DELETE" => {
            let paths = staged
                .iter()
                .filter(|file| file.op != "REMOVE")
                .map(|file| file.logical_path.clone())
                .collect::<Vec<_>>();
            if !paths.is_empty() {
                return Err(CommitError::DeleteWithWriteOps {
                    count: paths.len(),
                    paths,
                });
            }
        }
        other => {
            return Err(CommitError::UnknownKind {
                kind: other.to_string(),
            });
        }
    }
    Ok(())
}

fn apply_transaction(
    mut view: BTreeMap<String, ViewFile>,
    tx_type: &str,
    files: &[FileRow],
) -> Result<BTreeMap<String, ViewFile>, CommitError> {
    match tx_type {
        "SNAPSHOT" => {
            view.clear();
            for file in files.iter().filter(|file| file.op != "REMOVE") {
                view.insert(
                    file.logical_path.clone(),
                    ViewFile {
                        size_bytes: file.size_bytes,
                    },
                );
            }
        }
        "APPEND" => {
            for file in files.iter().filter(|file| file.op == "ADD") {
                view.entry(file.logical_path.clone()).or_insert(ViewFile {
                    size_bytes: file.size_bytes,
                });
            }
        }
        "UPDATE" => {
            for file in files {
                if file.op == "REMOVE" {
                    view.remove(&file.logical_path);
                } else {
                    view.insert(
                        file.logical_path.clone(),
                        ViewFile {
                            size_bytes: file.size_bytes,
                        },
                    );
                }
            }
        }
        "DELETE" => {
            for file in files {
                view.remove(&file.logical_path);
            }
        }
        other => {
            return Err(CommitError::UnknownKind {
                kind: other.to_string(),
            });
        }
    }
    Ok(view)
}
