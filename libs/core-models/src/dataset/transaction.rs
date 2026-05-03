//! Canonical Foundry-style dataset transaction primitives.
//!
//! This module is the single source of truth for the typed identifiers and
//! enums that describe dataset transactions across every OpenFoundry
//! service (data-asset-catalog, dataset-versioning, ingestion-replication,
//! pipeline-execution, ml-training, …). Services MUST consume these types
//! instead of redeclaring local strings or `Uuid` aliases so that the wire
//! format and Postgres encoding stay consistent.
//!
//! Wire/SQL encoding:
//! - [`TransactionId`] serializes as a UUID string.
//! - [`DatasetRid`] / [`BranchName`] serialize as plain strings.
//! - [`TransactionType`] / [`TransactionState`] serialize as lowercase
//!   strings (`"snapshot"`, `"committed"`, …) which matches the existing
//!   Postgres TEXT columns in `services/*/migrations/*.sql`.

use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Serialize};
use uuid::Uuid;

// ---------------------------------------------------------------------------
// Newtypes
// ---------------------------------------------------------------------------

/// Internal primary key of a dataset transaction (UUID v7 by convention).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct TransactionId(pub Uuid);

impl TransactionId {
    pub fn new() -> Self {
        Self(Uuid::now_v7())
    }

    pub fn into_inner(self) -> Uuid {
        self.0
    }
}

impl Default for TransactionId {
    fn default() -> Self {
        Self::new()
    }
}

impl fmt::Display for TransactionId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        self.0.fmt(f)
    }
}

impl From<Uuid> for TransactionId {
    fn from(value: Uuid) -> Self {
        Self(value)
    }
}

impl From<TransactionId> for Uuid {
    fn from(value: TransactionId) -> Self {
        value.0
    }
}

/// Foundry-style dataset Resource Identifier.
///
/// Format: `ri.foundry.main.dataset.<uuid-v7>` (lowercase). The wrapper
/// performs structural validation but does not enforce the UUID variant
/// beyond parseability.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct DatasetRid(pub String);

/// Error returned when a string cannot be parsed as a [`DatasetRid`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InvalidDatasetRid(pub String);

impl fmt::Display for InvalidDatasetRid {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "invalid dataset RID `{}` (expected `ri.foundry.main.dataset.<uuid>`)",
            self.0
        )
    }
}

impl std::error::Error for InvalidDatasetRid {}

impl DatasetRid {
    pub const PREFIX: &'static str = "ri.foundry.main.dataset.";

    /// Build a RID from a UUID, producing the canonical `ri.foundry…` form.
    pub fn from_uuid(uuid: Uuid) -> Self {
        Self(format!("{}{}", Self::PREFIX, uuid))
    }

    /// Mint a fresh RID backed by a new UUID v7.
    pub fn new() -> Self {
        Self::from_uuid(Uuid::now_v7())
    }

    /// Extract the UUID suffix, if the RID is well-formed.
    pub fn uuid(&self) -> Option<Uuid> {
        self.0
            .strip_prefix(Self::PREFIX)
            .and_then(|tail| Uuid::parse_str(tail).ok())
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl Default for DatasetRid {
    fn default() -> Self {
        Self::new()
    }
}

impl fmt::Display for DatasetRid {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl FromStr for DatasetRid {
    type Err = InvalidDatasetRid;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let tail = s
            .strip_prefix(Self::PREFIX)
            .ok_or_else(|| InvalidDatasetRid(s.to_string()))?;
        Uuid::parse_str(tail).map_err(|_| InvalidDatasetRid(s.to_string()))?;
        Ok(Self(s.to_string()))
    }
}

/// Branch name within a dataset (Foundry default: `master`).
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct BranchName(pub String);

/// Error returned when a branch name fails validation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InvalidBranchName {
    Empty,
    TooLong(usize),
    InvalidChar(char),
}

impl fmt::Display for InvalidBranchName {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empty => write!(f, "branch name must not be empty"),
            Self::TooLong(n) => write!(f, "branch name too long ({} chars, max 200)", n),
            Self::InvalidChar(c) => write!(
                f,
                "branch name contains invalid character `{}` (allowed: a-z A-Z 0-9 - _ / .)",
                c
            ),
        }
    }
}

impl std::error::Error for InvalidBranchName {}

impl BranchName {
    pub const MAX_LEN: usize = 200;

    /// The Foundry-default trunk branch name.
    pub fn master() -> Self {
        Self("master".to_string())
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    pub fn into_string(self) -> String {
        self.0
    }
}

impl Default for BranchName {
    fn default() -> Self {
        Self::master()
    }
}

impl fmt::Display for BranchName {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl FromStr for BranchName {
    type Err = InvalidBranchName;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        if s.is_empty() {
            return Err(InvalidBranchName::Empty);
        }
        if s.len() > Self::MAX_LEN {
            return Err(InvalidBranchName::TooLong(s.len()));
        }
        for ch in s.chars() {
            let ok = ch.is_ascii_alphanumeric() || matches!(ch, '-' | '_' | '/' | '.');
            if !ok {
                return Err(InvalidBranchName::InvalidChar(ch));
            }
        }
        Ok(Self(s.to_string()))
    }
}

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

/// Foundry-style transaction operation taxonomy.
///
/// Mirrors the textual values currently stored in the `operation` column of
/// the `dataset_transactions` Postgres tables.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TransactionType {
    /// Replaces the entire view contents (`SNAPSHOT` in Foundry).
    Snapshot,
    /// Adds new files/rows without rewriting prior data.
    Append,
    /// Logical row-level upsert (used by streaming + CDC).
    Update,
    /// Tombstone / removal of rows or files.
    Delete,
}

impl TransactionType {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Snapshot => "snapshot",
            Self::Append => "append",
            Self::Update => "update",
            Self::Delete => "delete",
        }
    }
}

impl fmt::Display for TransactionType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

impl FromStr for TransactionType {
    type Err = UnknownTransactionType;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_ascii_lowercase().as_str() {
            "snapshot" => Ok(Self::Snapshot),
            "append" => Ok(Self::Append),
            "update" => Ok(Self::Update),
            "delete" => Ok(Self::Delete),
            other => Err(UnknownTransactionType(other.to_string())),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UnknownTransactionType(pub String);

impl fmt::Display for UnknownTransactionType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "unknown transaction type `{}`", self.0)
    }
}

impl std::error::Error for UnknownTransactionType {}

/// Lifecycle state of a dataset transaction.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TransactionState {
    /// Started but not yet committed; writes are staged.
    Open,
    /// Successfully sealed; visible to readers.
    Committed,
    /// Cancelled or failed; staged writes are discarded.
    Aborted,
}

impl TransactionState {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Open => "open",
            Self::Committed => "committed",
            Self::Aborted => "aborted",
        }
    }

    /// Whether this is a terminal state (no further transitions allowed).
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Committed | Self::Aborted)
    }
}

impl fmt::Display for TransactionState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

impl FromStr for TransactionState {
    type Err = UnknownTransactionState;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_ascii_lowercase().as_str() {
            "open" => Ok(Self::Open),
            "committed" => Ok(Self::Committed),
            "aborted" => Ok(Self::Aborted),
            other => Err(UnknownTransactionState(other.to_string())),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UnknownTransactionState(pub String);

impl fmt::Display for UnknownTransactionState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "unknown transaction state `{}`", self.0)
    }
}

impl std::error::Error for UnknownTransactionState {}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dataset_rid_roundtrip() {
        let uuid = Uuid::now_v7();
        let rid = DatasetRid::from_uuid(uuid);
        assert!(rid.as_str().starts_with(DatasetRid::PREFIX));
        assert_eq!(rid.uuid(), Some(uuid));
        let parsed: DatasetRid = rid.as_str().parse().expect("valid rid");
        assert_eq!(parsed, rid);
    }

    #[test]
    fn dataset_rid_rejects_bad_input() {
        assert!("ri.foundry.main.folder.123".parse::<DatasetRid>().is_err());
        assert!(
            "ri.foundry.main.dataset.not-a-uuid"
                .parse::<DatasetRid>()
                .is_err()
        );
    }

    #[test]
    fn branch_name_validation() {
        assert!("master".parse::<BranchName>().is_ok());
        assert!("feature/streaming-v2".parse::<BranchName>().is_ok());
        assert!("".parse::<BranchName>().is_err());
        assert!("bad name".parse::<BranchName>().is_err());
    }

    #[test]
    fn transaction_type_serde() {
        let json = serde_json::to_string(&TransactionType::Snapshot).unwrap();
        assert_eq!(json, "\"snapshot\"");
        let parsed: TransactionType = serde_json::from_str("\"append\"").unwrap();
        assert_eq!(parsed, TransactionType::Append);
        assert_eq!(
            "delete".parse::<TransactionType>().unwrap(),
            TransactionType::Delete
        );
    }

    #[test]
    fn transaction_state_terminal() {
        assert!(!TransactionState::Open.is_terminal());
        assert!(TransactionState::Committed.is_terminal());
        assert!(TransactionState::Aborted.is_terminal());
    }
}
