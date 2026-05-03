//! Transactional dataset writer facade (T1.4).
//!
//! Wraps the lower-level [`DatasetWriter`] backends (legacy / Iceberg) with
//! the Foundry transaction lifecycle:
//!
//! ```text
//!   open_transaction (kind=SNAPSHOT|APPEND|UPDATE|DELETE)
//!         │
//!         ├── stage_file{,_remove} … N times
//!         │       (logical_path  → key visible to readers,
//!         │        physical_path → backing-store key returned by the writer)
//!         │
//!         ├── commit  → atomic SQL: validate invariants
//!         │             + UPDATE status='COMMITTED'
//!         │             + UPDATE dataset_branches.head_transaction_id
//!         │             (single sqlx::Transaction, see
//!         │              `domain::transactions::commit_transaction`).
//!         └── abort   → atomic SQL: UPDATE status='ABORTED'.
//! ```
//!
//! The split between `logical_path` and `physical_path` honours the
//! "Backing filesystem" section of `Datasets.md`: callers stage logical
//! file keys; the underlying [`DatasetWriter`] decides where the bytes
//! land on disk / object store.

use sqlx::PgPool;
use uuid::Uuid;

use core_models::TransactionType;
use serde_json::Value;

use crate::domain::transactions::{self as txn_domain, CommitError};
use crate::storage::runtime::{OpenTransactionInsert, RuntimeStore};

/// Per-file op code persisted in `dataset_transaction_files.op`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StageOp {
    Add,
    Replace,
    Remove,
}

impl StageOp {
    fn as_db(&self) -> &'static str {
        match self {
            Self::Add => "ADD",
            Self::Replace => "REPLACE",
            Self::Remove => "REMOVE",
        }
    }
}

/// A staged file inside an OPEN transaction.
#[derive(Debug, Clone)]
pub struct StagedFile {
    /// Dataset-relative key visible to readers (e.g. `data/part-0.parquet`).
    pub logical_path: String,
    /// Backing-store key produced by the writer (e.g. the parquet path under
    /// the storage backend, or `iceberg://ns/table#snap`).
    pub physical_path: String,
    pub size_bytes: i64,
    pub op: StageOp,
}

/// Handle returned by [`TransactionalDatasetWriter::open_transaction`].
///
/// The handle is a value-object: callers may keep multiple in flight
/// (e.g. one per branch) without holding onto the writer mutably.
#[derive(Debug, Clone, Copy)]
pub struct OpenTransaction {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub branch_id: Uuid,
    pub kind: TransactionType,
}

/// Facade exposing `open_transaction` / `commit` / `abort`.
///
/// Atomicity guarantees:
/// * `commit` runs all DB mutations inside a single `sqlx::Transaction`
///   (see `domain::transactions::commit_transaction`): validate invariants,
///   flip status to `COMMITTED`, advance the branch HEAD pointer, set
///   per-kind dataset metadata. Any failure rolls everything back.
/// * `abort` is similarly atomic.
/// * `open_transaction` relies on the partial unique index
///   `uq_dataset_transactions_one_open_per_branch` to reject concurrent
///   opens on the same branch with `409 Conflict`.
#[derive(Debug, Clone)]
pub struct TransactionalDatasetWriter {
    pool: PgPool,
}

impl TransactionalDatasetWriter {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }

    fn kind_db(kind: TransactionType) -> &'static str {
        match kind {
            TransactionType::Snapshot => "SNAPSHOT",
            TransactionType::Append => "APPEND",
            TransactionType::Update => "UPDATE",
            TransactionType::Delete => "DELETE",
        }
    }

    /// Open a new transaction on `(dataset_id, branch_id)` of the given
    /// `kind`.
    ///
    /// Errors with [`CommitError::ConcurrentOpenTransaction`] when the branch
    /// already has an OPEN transaction.
    pub async fn open_transaction(
        &self,
        dataset_id: Uuid,
        branch_id: Uuid,
        branch_name: &str,
        kind: TransactionType,
        summary: &str,
        providence: &Value,
        started_by: Option<Uuid>,
    ) -> Result<OpenTransaction, CommitError> {
        let id = Uuid::now_v7();
        RuntimeStore::new(self.pool.clone())
            .insert_open_transaction(OpenTransactionInsert {
                id,
                dataset_id,
                branch_id,
                branch_name,
                tx_type: Self::kind_db(kind),
                operation: Self::kind_db(kind).to_ascii_lowercase(),
                summary,
                providence,
                started_by,
            })
            .await
            .map_err(|error| match error {
                sqlx::Error::Database(db)
                    if db.constraint() == Some("uq_dataset_transactions_one_open_per_branch") =>
                {
                    CommitError::ConcurrentOpenTransaction {
                        branch: branch_name.to_string(),
                    }
                }
                other => CommitError::from(other),
            })?;

        Ok(OpenTransaction {
            id,
            dataset_id,
            branch_id,
            kind,
        })
    }

    /// Stage a file inside the OPEN transaction.
    ///
    /// `logical_path` and `physical_path` are persisted independently so
    /// later compaction/Iceberg snapshotting can rewrite the physical
    /// backing without disturbing dataset-relative keys.
    pub async fn stage_file(
        &self,
        txn: &OpenTransaction,
        file: StagedFile,
    ) -> Result<(), CommitError> {
        sqlx::query(
            r#"INSERT INTO dataset_transaction_files
                   (transaction_id, logical_path, physical_path, size_bytes, op)
               VALUES ($1, $2, $3, $4, $5)
               ON CONFLICT (transaction_id, logical_path) DO UPDATE
                  SET physical_path = EXCLUDED.physical_path,
                      size_bytes    = EXCLUDED.size_bytes,
                      op            = EXCLUDED.op"#,
        )
        .bind(txn.id)
        .bind(&file.logical_path)
        .bind(&file.physical_path)
        .bind(file.size_bytes)
        .bind(file.op.as_db())
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    /// Commit the transaction atomically: validates invariants and applies
    /// the SQL mutations (status flip + branch HEAD update + metadata
    /// flags) in a single Postgres transaction.
    pub async fn commit(&self, txn: &OpenTransaction) -> Result<Uuid, CommitError> {
        txn_domain::commit_transaction(&self.pool, txn.id).await?;
        Ok(txn.id)
    }

    /// Abort the transaction atomically.
    pub async fn abort(&self, txn: &OpenTransaction) -> Result<Uuid, CommitError> {
        txn_domain::abort_transaction(&self.pool, txn.id).await?;
        Ok(txn.id)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stage_op_db_encoding() {
        assert_eq!(StageOp::Add.as_db(), "ADD");
        assert_eq!(StageOp::Replace.as_db(), "REPLACE");
        assert_eq!(StageOp::Remove.as_db(), "REMOVE");
    }

    #[test]
    fn kind_db_encoding_matches_check_constraint() {
        // Mirrors `dataset_transactions.tx_type IN ('SNAPSHOT','APPEND','UPDATE','DELETE')`
        // from migrations/20260501000001_versioning_init.sql
        assert_eq!(
            TransactionalDatasetWriter::kind_db(TransactionType::Snapshot),
            "SNAPSHOT"
        );
        assert_eq!(
            TransactionalDatasetWriter::kind_db(TransactionType::Append),
            "APPEND"
        );
        assert_eq!(
            TransactionalDatasetWriter::kind_db(TransactionType::Update),
            "UPDATE"
        );
        assert_eq!(
            TransactionalDatasetWriter::kind_db(TransactionType::Delete),
            "DELETE"
        );
    }
}
