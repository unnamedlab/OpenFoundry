//! Iceberg snapshot type ↔ Foundry transaction type mapping.
//!
//! From `Iceberg tables/Transactions.md` § "Iceberg snapshot types and
//! Foundry dataset transactions":
//!
//! | Iceberg snapshot   | Foundry transaction          |
//! |--------------------|------------------------------|
//! | Append snapshot    | APPEND                       |
//! | Overwrite snapshot | UPDATE *or* SNAPSHOT         |
//! | Delete snapshot    | DELETE                       |
//! | Replace snapshot   | *(no equivalent — compaction)*|
//!
//! Two heuristics are encoded here:
//!
//! 1. **First write to an empty table is always an `Append`**, even
//!    when the user's intent is to fully replace the table. Iceberg
//!    only records what physically happened (files added, none
//!    removed) so the snapshot type follows that.
//! 2. **Overwrite vs SNAPSHOT** depends on whether the operation
//!    removes *every* previous data file or only a subset. We mirror
//!    Foundry's behaviour: a full sweep is `SNAPSHOT`, a partial sweep
//!    is `UPDATE`. The decision lives in [`overwrite_kind`] so the
//!    rest of the codebase doesn't have to repeat the rule.

use serde::{Deserialize, Serialize};

/// Foundry's catalog transaction taxonomy. See
/// [`docs/reference-tables/dataset_transactions.md`] in the Foundry
/// docs for the long-form description.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "UPPERCASE")]
pub enum FoundryTransactionType {
    Append,
    Update,
    Snapshot,
    Delete,
    /// Internal operations (compaction, manifest rewrites) that do not
    /// surface as a Foundry transaction. The catalog still records the
    /// Iceberg snapshot but the Foundry transaction model treats them
    /// as no-ops.
    InternalNoop,
}

impl FoundryTransactionType {
    pub fn as_str(self) -> &'static str {
        match self {
            FoundryTransactionType::Append => "APPEND",
            FoundryTransactionType::Update => "UPDATE",
            FoundryTransactionType::Snapshot => "SNAPSHOT",
            FoundryTransactionType::Delete => "DELETE",
            FoundryTransactionType::InternalNoop => "INTERNAL_NOOP",
        }
    }
}

/// Iceberg snapshot operations as written to `metadata.json` per spec.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum IcebergOperation {
    Append,
    Overwrite,
    Delete,
    Replace,
}

impl IcebergOperation {
    pub fn as_str(self) -> &'static str {
        match self {
            IcebergOperation::Append => "append",
            IcebergOperation::Overwrite => "overwrite",
            IcebergOperation::Delete => "delete",
            IcebergOperation::Replace => "replace",
        }
    }

    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "append" => Some(IcebergOperation::Append),
            "overwrite" => Some(IcebergOperation::Overwrite),
            "delete" => Some(IcebergOperation::Delete),
            "replace" => Some(IcebergOperation::Replace),
            _ => None,
        }
    }
}

/// Hint about an Overwrite snapshot's scope, computed from the diff
/// between the snapshot summary's `removed-data-files` and the
/// table's previous total file count.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OverwriteKind {
    /// Removed every previous file → looks like a SNAPSHOT to Foundry.
    Full,
    /// Removed a subset → looks like an UPDATE to Foundry.
    Partial,
}

/// Compute whether an Overwrite operation should be classified as Full
/// (SNAPSHOT) or Partial (UPDATE) given the snapshot summary +
/// pre-write file count.
pub fn overwrite_kind(removed_data_files: u64, previous_total_files: u64) -> OverwriteKind {
    if previous_total_files > 0 && removed_data_files >= previous_total_files {
        OverwriteKind::Full
    } else {
        OverwriteKind::Partial
    }
}

/// Map an Iceberg snapshot to its Foundry equivalent.
///
/// `previous_total_files` is the number of data files in the table
/// **before** the snapshot landed. Pass `0` if the table was empty
/// (first write).
pub fn iceberg_to_foundry(
    op: IcebergOperation,
    previous_total_files: u64,
    removed_data_files: u64,
) -> FoundryTransactionType {
    match op {
        // Special case (per doc): the first write to an empty table is
        // always an Append snapshot. Foundry preserves that mapping.
        IcebergOperation::Append => FoundryTransactionType::Append,
        IcebergOperation::Overwrite => match overwrite_kind(removed_data_files, previous_total_files) {
            OverwriteKind::Full => FoundryTransactionType::Snapshot,
            OverwriteKind::Partial => FoundryTransactionType::Update,
        },
        IcebergOperation::Delete => FoundryTransactionType::Delete,
        IcebergOperation::Replace => FoundryTransactionType::InternalNoop,
    }
}

/// Map a Foundry transaction type to the Iceberg snapshot it should
/// produce when committed against an Iceberg-backed table.
///
/// `table_is_empty` lets the caller honour the doc's invariant:
///   "Primera escritura a tabla vacía SIEMPRE es Append, incluso si la
///    intención era SNAPSHOT".
pub fn foundry_to_iceberg(t: FoundryTransactionType, table_is_empty: bool) -> IcebergOperation {
    if table_is_empty
        && matches!(
            t,
            FoundryTransactionType::Snapshot
                | FoundryTransactionType::Update
                | FoundryTransactionType::Append
        )
    {
        return IcebergOperation::Append;
    }
    match t {
        FoundryTransactionType::Append => IcebergOperation::Append,
        // Both UPDATE (partial sweep) and SNAPSHOT (full sweep) project
        // back to an Iceberg Overwrite — the difference is encoded in
        // how many data files the commit removes, not in the operation
        // name.
        FoundryTransactionType::Update | FoundryTransactionType::Snapshot => {
            IcebergOperation::Overwrite
        }
        FoundryTransactionType::Delete => IcebergOperation::Delete,
        // Internal operations never originate from Foundry user code
        // but `replace` is what the maintenance worker emits.
        FoundryTransactionType::InternalNoop => IcebergOperation::Replace,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn append_maps_to_append_in_both_directions() {
        assert_eq!(
            iceberg_to_foundry(IcebergOperation::Append, 10, 0),
            FoundryTransactionType::Append
        );
        assert_eq!(
            foundry_to_iceberg(FoundryTransactionType::Append, false),
            IcebergOperation::Append
        );
    }

    #[test]
    fn overwrite_full_sweep_maps_to_snapshot() {
        assert_eq!(
            iceberg_to_foundry(IcebergOperation::Overwrite, 8, 8),
            FoundryTransactionType::Snapshot
        );
    }

    #[test]
    fn overwrite_partial_sweep_maps_to_update() {
        assert_eq!(
            iceberg_to_foundry(IcebergOperation::Overwrite, 8, 3),
            FoundryTransactionType::Update
        );
    }

    #[test]
    fn delete_maps_to_delete() {
        assert_eq!(
            iceberg_to_foundry(IcebergOperation::Delete, 10, 0),
            FoundryTransactionType::Delete
        );
        assert_eq!(
            foundry_to_iceberg(FoundryTransactionType::Delete, false),
            IcebergOperation::Delete
        );
    }

    #[test]
    fn replace_maps_to_internal_noop() {
        assert_eq!(
            iceberg_to_foundry(IcebergOperation::Replace, 10, 8),
            FoundryTransactionType::InternalNoop
        );
    }

    #[test]
    fn snapshot_intent_on_empty_table_is_recorded_as_append() {
        // Doc invariant: first write is always Append even if the
        // user's intent was a full SNAPSHOT.
        assert_eq!(
            foundry_to_iceberg(FoundryTransactionType::Snapshot, true),
            IcebergOperation::Append
        );
        assert_eq!(
            foundry_to_iceberg(FoundryTransactionType::Update, true),
            IcebergOperation::Append
        );
    }

    #[test]
    fn snapshot_on_non_empty_table_is_overwrite() {
        assert_eq!(
            foundry_to_iceberg(FoundryTransactionType::Snapshot, false),
            IcebergOperation::Overwrite
        );
    }

    #[test]
    fn overwrite_kind_handles_zero_total_files_safely() {
        // Defensive guard: a summary that claims it removed files from
        // a table with zero recorded files is treated as Partial so we
        // don't accidentally project to SNAPSHOT for a first write.
        assert_eq!(overwrite_kind(0, 0), OverwriteKind::Partial);
    }
}
