//! `media_set_transactions` row type + REST DTOs.

use std::str::FromStr;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TransactionState {
    Open,
    Committed,
    Aborted,
}

impl TransactionState {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Open => "OPEN",
            Self::Committed => "COMMITTED",
            Self::Aborted => "ABORTED",
        }
    }

    pub fn is_terminal(self) -> bool {
        matches!(self, Self::Committed | Self::Aborted)
    }
}

impl FromStr for TransactionState {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Ok(match s {
            "OPEN" => Self::Open,
            "COMMITTED" => Self::Committed,
            "ABORTED" => Self::Aborted,
            other => return Err(format!("unknown TransactionState `{other}`")),
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct MediaSetTransaction {
    pub rid: String,
    pub media_set_rid: String,
    pub branch: String,
    pub state: String,
    pub opened_at: DateTime<Utc>,
    pub closed_at: Option<DateTime<Utc>>,
    pub opened_by: String,
}

/// History row surfaced by `GET /media-sets/{rid}/transactions`. The
/// per-transaction diff lets the History tab render added / removed /
/// modified counts without a follow-up roundtrip.
///
/// Counting semantics:
///   * `items_added`     — rows inserted in this transaction whose
///                         `deduplicated_from` is NULL.
///   * `items_modified`  — rows inserted in this transaction whose
///                         `deduplicated_from` points at a prior row
///                         on the same branch (i.e. path-dedup hit).
///   * `items_deleted`   — rows soft-deleted by this transaction
///                         (the `deduplicated_from` ancestor of any
///                         "modified" row, plus rows soft-deleted by
///                         REPLACE-mode commits).
#[derive(Debug, Clone, Serialize, FromRow)]
pub struct TransactionHistoryEntry {
    pub rid: String,
    pub media_set_rid: String,
    pub branch: String,
    pub state: String,
    pub write_mode: String,
    pub opened_at: DateTime<Utc>,
    pub closed_at: Option<DateTime<Utc>>,
    pub opened_by: String,
    pub items_added: i64,
    pub items_modified: i64,
    pub items_deleted: i64,
}

/// Foundry "media set write modes" per
/// `Transforms/Python (Spark)/Incremental transforms/Incremental media sets.md`.
///
/// * `Modify` (default) — appends + path-dedups. Both transactionless
///   and transactional sets accept it.
/// * `Replace` — surfaces only the items written in the transaction;
///   every prior live item on the branch becomes inaccessible.
///   **Transactional sets only** — transactionless sets reject it
///   with `MEDIA_SET_TRANSACTIONLESS_REJECTS_REPLACE` (422).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum WriteMode {
    #[default]
    Modify,
    Replace,
}

impl WriteMode {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Modify => "MODIFY",
            Self::Replace => "REPLACE",
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct OpenTransactionBody {
    /// Defaults to `"main"`.
    #[serde(default)]
    pub branch: Option<String>,
    /// Defaults to `MODIFY`. `REPLACE` is rejected on
    /// transactionless sets (Foundry "Incremental write modes").
    #[serde(default)]
    pub write_mode: Option<WriteMode>,
}
