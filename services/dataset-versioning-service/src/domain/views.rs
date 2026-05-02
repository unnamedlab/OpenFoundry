//! Pure dataset-view computation algorithm (T1.3).
//!
//! Implements the Foundry "Dataset views" algorithm:
//!
//! 1. Start from an empty file set.
//! 2. Locate the **last** `SNAPSHOT` whose timestamp is `≤ at_ts`. If none
//!    exists, replay every committed transaction from the oldest one.
//! 3. From that anchor, replay each subsequent committed transaction in
//!    timestamp order applying:
//!      * `SNAPSHOT` / `APPEND` → insert/overwrite the file in the set.
//!      * `UPDATE`              → overwrite (or insert) the file.
//!      * `DELETE`              → drop the file from the set.
//!
//! The function is intentionally **pure**: it consumes a flat
//! `Vec<TransactionEntry>` and a cut-off timestamp and returns the
//! deterministic file set. Postgres I/O, branch resolution and view
//! caching live in the handlers / DB layer (`handlers::foundry`,
//! `domain::transactions`).

use std::collections::BTreeMap;

use chrono::{DateTime, Utc};
use core_models::TransactionType;
use uuid::Uuid;

/// Reference to a single physical file inside a dataset view.
///
/// `logical_path` is the dataset-relative key (stable across snapshots and
/// transactions). `physical_path` is the backing-store key produced by the
/// writer; the two are decoupled so that compaction / Iceberg snapshotting
/// can rewrite files without breaking view stability (see
/// `Datasets.md` § "Backing filesystem").
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FileRef {
    pub logical_path: String,
    pub physical_path: String,
    pub size_bytes: i64,
    pub introduced_by: Uuid,
}

/// Per-file op staged inside a transaction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FileOp {
    pub logical_path: String,
    pub physical_path: String,
    pub size_bytes: i64,
    pub kind: FileOpKind,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FileOpKind {
    /// New file added by the transaction (APPEND / SNAPSHOT).
    Add,
    /// Existing file overwritten (UPDATE / SNAPSHOT-after-collision).
    Replace,
    /// File logically removed (DELETE). `physical_path` is preserved so
    /// auditors can still locate the orphaned blob.
    Remove,
}

/// One committed transaction ready to be replayed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransactionEntry {
    pub txn_id: Uuid,
    pub kind: TransactionType,
    /// Effective timestamp used for ordering (`COALESCE(committed_at, started_at)`).
    pub committed_at: DateTime<Utc>,
    pub files: Vec<FileOp>,
}

/// Compute the current view of a dataset/branch as of `at_ts`.
///
/// `transactions` MUST contain only `COMMITTED` transactions for the
/// branch and MUST be sorted by `committed_at` ascending. Aborted/open
/// transactions and other branches must be filtered out by the caller.
pub fn compute_view(
    transactions: &[TransactionEntry],
    at_ts: Option<DateTime<Utc>>,
) -> Vec<FileRef> {
    // 1. Trim by cut-off.
    let in_window: Vec<&TransactionEntry> = transactions
        .iter()
        .filter(|t| at_ts.map_or(true, |cut| t.committed_at <= cut))
        .collect();

    if in_window.is_empty() {
        return Vec::new();
    }

    // 2. Anchor at the last SNAPSHOT inside the window (or 0 if none).
    let start_idx = in_window
        .iter()
        .rposition(|t| t.kind == TransactionType::Snapshot)
        .unwrap_or(0);

    // 3. Replay forward.  BTreeMap → deterministic ordering by logical_path.
    let mut files: BTreeMap<String, FileRef> = BTreeMap::new();

    for entry in in_window.iter().skip(start_idx) {
        match entry.kind {
            TransactionType::Snapshot => {
                files.clear();
                for op in &entry.files {
                    // SNAPSHOT carries Add/Replace ops only (REMOVE rejected
                    // upstream by domain::transactions::validate_commit).
                    files.insert(
                        op.logical_path.clone(),
                        FileRef {
                            logical_path: op.logical_path.clone(),
                            physical_path: op.physical_path.clone(),
                            size_bytes: op.size_bytes,
                            introduced_by: entry.txn_id,
                        },
                    );
                }
            }
            TransactionType::Append => {
                for op in &entry.files {
                    // APPEND must not collide with existing files (enforced
                    // at commit time); we use `entry()` so the algorithm
                    // remains correct even if invariants are bypassed.
                    files.entry(op.logical_path.clone()).or_insert(FileRef {
                        logical_path: op.logical_path.clone(),
                        physical_path: op.physical_path.clone(),
                        size_bytes: op.size_bytes,
                        introduced_by: entry.txn_id,
                    });
                }
            }
            TransactionType::Update => {
                for op in &entry.files {
                    files.insert(
                        op.logical_path.clone(),
                        FileRef {
                            logical_path: op.logical_path.clone(),
                            physical_path: op.physical_path.clone(),
                            size_bytes: op.size_bytes,
                            introduced_by: entry.txn_id,
                        },
                    );
                }
            }
            TransactionType::Delete => {
                for op in &entry.files {
                    files.remove(&op.logical_path);
                }
            }
        }
    }

    files.into_values().collect()
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — covers the Datasets.md worked example and the edge cases listed in
// the issue.
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn ts(secs: i64) -> DateTime<Utc> {
        Utc.timestamp_opt(secs, 0).unwrap()
    }

    fn add(logical: &str, physical: &str) -> FileOp {
        FileOp {
            logical_path: logical.to_string(),
            physical_path: physical.to_string(),
            size_bytes: 1,
            kind: FileOpKind::Add,
        }
    }
    fn replace(logical: &str, physical: &str) -> FileOp {
        FileOp {
            logical_path: logical.to_string(),
            physical_path: physical.to_string(),
            size_bytes: 1,
            kind: FileOpKind::Replace,
        }
    }
    fn remove(logical: &str) -> FileOp {
        FileOp {
            logical_path: logical.to_string(),
            physical_path: String::new(),
            size_bytes: 0,
            kind: FileOpKind::Remove,
        }
    }

    fn entry(secs: i64, kind: TransactionType, files: Vec<FileOp>) -> TransactionEntry {
        TransactionEntry {
            txn_id: Uuid::now_v7(),
            kind,
            committed_at: ts(secs),
            files,
        }
    }

    /// The worked example from `Datasets.md`:
    /// `[SNAPSHOT(A,B), APPEND(C), UPDATE(A→A'), DELETE(B)] ⇒ {A',C}`.
    #[test]
    fn doc_example_snapshot_append_update_delete() {
        let txns = vec![
            entry(
                10,
                TransactionType::Snapshot,
                vec![add("A", "phys/A.v0"), add("B", "phys/B.v0")],
            ),
            entry(20, TransactionType::Append, vec![add("C", "phys/C.v0")]),
            entry(
                30,
                TransactionType::Update,
                vec![replace("A", "phys/A.v1")],
            ),
            entry(40, TransactionType::Delete, vec![remove("B")]),
        ];

        let view = compute_view(&txns, None);
        let logical: Vec<&str> = view.iter().map(|f| f.logical_path.as_str()).collect();
        assert_eq!(logical, vec!["A", "C"]);

        let a = view.iter().find(|f| f.logical_path == "A").unwrap();
        assert_eq!(a.physical_path, "phys/A.v1", "A must be the UPDATEd version");
    }

    /// A trailing SNAPSHOT must wipe everything before it.
    #[test]
    fn trailing_snapshot_resets_view() {
        let txns = vec![
            entry(
                10,
                TransactionType::Snapshot,
                vec![add("A", "phys/A.v0"), add("B", "phys/B.v0")],
            ),
            entry(20, TransactionType::Append, vec![add("C", "phys/C.v0")]),
            entry(
                30,
                TransactionType::Update,
                vec![replace("A", "phys/A.v1")],
            ),
            entry(40, TransactionType::Delete, vec![remove("B")]),
            entry(50, TransactionType::Snapshot, vec![add("D", "phys/D.v0")]),
        ];

        let view = compute_view(&txns, None);
        let logical: Vec<&str> = view.iter().map(|f| f.logical_path.as_str()).collect();
        assert_eq!(logical, vec!["D"]);
    }

    /// Time-travel: cut-off before the trailing SNAPSHOT must yield the
    /// original `{A', C}` view.
    #[test]
    fn at_ts_before_trailing_snapshot() {
        let txns = vec![
            entry(
                10,
                TransactionType::Snapshot,
                vec![add("A", "phys/A.v0"), add("B", "phys/B.v0")],
            ),
            entry(20, TransactionType::Append, vec![add("C", "phys/C.v0")]),
            entry(
                30,
                TransactionType::Update,
                vec![replace("A", "phys/A.v1")],
            ),
            entry(40, TransactionType::Delete, vec![remove("B")]),
            entry(50, TransactionType::Snapshot, vec![add("D", "phys/D.v0")]),
        ];

        let view = compute_view(&txns, Some(ts(45)));
        let logical: Vec<&str> = view.iter().map(|f| f.logical_path.as_str()).collect();
        assert_eq!(logical, vec!["A", "C"]);
    }

    /// Empty input or cut-off before the first txn yields an empty view.
    #[test]
    fn empty_view_when_no_transactions_in_window() {
        let txns = vec![entry(
            100,
            TransactionType::Snapshot,
            vec![add("A", "phys/A")],
        )];
        assert!(compute_view(&txns, Some(ts(50))).is_empty());
        assert!(compute_view(&[], None).is_empty());
    }

    /// When no SNAPSHOT exists, replay starts at the oldest txn (Foundry
    /// semantics: the first transaction is implicitly anchoring).
    #[test]
    fn no_snapshot_replays_from_oldest() {
        let txns = vec![
            entry(10, TransactionType::Append, vec![add("A", "phys/A")]),
            entry(20, TransactionType::Append, vec![add("B", "phys/B")]),
            entry(30, TransactionType::Update, vec![replace("A", "phys/A.v1")]),
        ];
        let view = compute_view(&txns, None);
        let logical: Vec<&str> = view.iter().map(|f| f.logical_path.as_str()).collect();
        assert_eq!(logical, vec!["A", "B"]);
        assert_eq!(view[0].physical_path, "phys/A.v1");
    }

    /// `physical_path` is decoupled from `logical_path`: an UPDATE rewrites
    /// the backing file but the dataset-relative key is unchanged.
    #[test]
    fn physical_path_decoupled_from_logical() {
        let txns = vec![
            entry(10, TransactionType::Snapshot, vec![add("data/part-0.parquet", "store/abcd.parquet")]),
            entry(
                20,
                TransactionType::Update,
                vec![replace("data/part-0.parquet", "store/efgh.parquet")],
            ),
        ];
        let view = compute_view(&txns, None);
        assert_eq!(view.len(), 1);
        assert_eq!(view[0].logical_path, "data/part-0.parquet");
        assert_eq!(view[0].physical_path, "store/efgh.parquet");
    }
}
