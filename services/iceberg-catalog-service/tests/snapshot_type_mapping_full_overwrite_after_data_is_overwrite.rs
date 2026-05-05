//! Once a table holds data, a SNAPSHOT (full sweep) commits as an
//! Iceberg overwrite — and the reverse mapping projects an Iceberg
//! Overwrite that removed every previous file back to `SNAPSHOT`.

use iceberg_catalog_service::domain::snapshot_mapping::{
    FoundryTransactionType, IcebergOperation, foundry_to_iceberg, iceberg_to_foundry,
    overwrite_kind, OverwriteKind,
};

#[test]
fn snapshot_intent_against_non_empty_table_is_iceberg_overwrite() {
    assert_eq!(
        foundry_to_iceberg(FoundryTransactionType::Snapshot, false),
        IcebergOperation::Overwrite
    );
}

#[test]
fn full_sweep_overwrite_classifies_back_to_snapshot() {
    // Removed all 8 of 8 previous files → full sweep.
    assert_eq!(
        iceberg_to_foundry(IcebergOperation::Overwrite, 8, 8),
        FoundryTransactionType::Snapshot
    );
    assert_eq!(overwrite_kind(8, 8), OverwriteKind::Full);
}

#[test]
fn partial_sweep_overwrite_classifies_back_to_update() {
    // Removed 3 of 8 previous files → partial.
    assert_eq!(
        iceberg_to_foundry(IcebergOperation::Overwrite, 8, 3),
        FoundryTransactionType::Update
    );
    assert_eq!(overwrite_kind(3, 8), OverwriteKind::Partial);
}
