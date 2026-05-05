//! Per Foundry doc § "Why does a first write appear as an append
//! snapshot?": the first write to an empty table must always project
//! to `Append`, even when the user's intent was `SNAPSHOT` /
//! `UPDATE`.

use iceberg_catalog_service::domain::snapshot_mapping::{
    FoundryTransactionType, IcebergOperation, foundry_to_iceberg, iceberg_to_foundry,
};

#[test]
fn first_write_with_snapshot_intent_maps_to_iceberg_append() {
    assert_eq!(
        foundry_to_iceberg(FoundryTransactionType::Snapshot, true),
        IcebergOperation::Append
    );
}

#[test]
fn first_write_with_update_intent_maps_to_iceberg_append() {
    assert_eq!(
        foundry_to_iceberg(FoundryTransactionType::Update, true),
        IcebergOperation::Append
    );
}

#[test]
fn first_write_with_append_intent_stays_append() {
    assert_eq!(
        foundry_to_iceberg(FoundryTransactionType::Append, true),
        IcebergOperation::Append
    );
}

#[test]
fn iceberg_first_append_is_always_recorded_as_foundry_append() {
    // previous_total_files == 0 → empty table.
    assert_eq!(
        iceberg_to_foundry(IcebergOperation::Append, 0, 0),
        FoundryTransactionType::Append
    );
}
